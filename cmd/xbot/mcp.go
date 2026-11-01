package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/mcp"
)

const mcpUsage = `Usage: xbot [--root DIR] mcp ACTION [names] (--self | --http ADDR --bot NAME) [--json]
  list [--scope global|bot]
  add SERVER (--command PATH [--arg VALUE]... | --url URL) [--secret-env NAME]... [--scope global|bot]
  remove SERVER [--scope global|bot]
  tools SERVER
  describe SERVER REMOTE_TOOL
  call SERVER REMOTE_TOOL [--args JSON_OBJECT | --args-file FILE|-] [--readonly] [--timeout DURATION]
Default scope is global; bot is an entire local override. Runtime commands require a running instance.
--self uses this source's XBOT_ENDPOINT and XBOT_ROOT. Human commands require explicit --http and --bot.
--timeout defaults to 0 (caller signal context). MCP discovery remains bounded separately.
Results are bounded JSON or local workspace file references. Tool is the original remote name.
Offline file configuration: xbot [--root DIR] mcp offline list|add|remove BOT [SERVER] [transport flags].
`

func cmdMCP(rawRoot string, args []string) error { return runMCP(rawRoot, args, os.Stdin, os.Stdout) }
func runMCP(rawRoot string, args []string, in io.Reader, out io.Writer) error {
	command, err := cliops.ParseMCP(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, mcpUsage)
		return err
	}
	if command.Offline {
		return runMCPOffline(rawRoot, command, out)
	}
	arguments := json.RawMessage(`{}`)
	if command.Action == "call" {
		if command.ArgsSet {
			arguments = json.RawMessage(command.Args)
		}
		if command.ArgsFile != "" {
			reader := in
			if command.ArgsFile != "-" {
				file, err := os.Open(command.ArgsFile)
				if err != nil {
					return err
				}
				defer file.Close()
				reader = file
			}
			arguments, err = io.ReadAll(io.LimitReader(reader, cliops.MCPMaxRequest+1))
			if err != nil {
				return err
			}
		}
		if err := cliops.ValidateMCPArgs(arguments); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if command.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, command.Timeout)
		defer cancel()
	}
	var client *http.Client
	var base, prefix, root string
	if command.Self {
		base = os.Getenv("XBOT_ENDPOINT")
		root = os.Getenv("XBOT_ROOT")
		if root == "" {
			return errors.New("mcp: --self requires XBOT_ROOT from an active runtime")
		}
		endpoint, err := url.Parse(base)
		if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || endpoint.Port() == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return errors.New("mcp: --self requires an active local XBot endpoint")
		}
		root, err = cliops.CanonicalRoot(root)
		if err != nil {
			return fmt.Errorf("mcp: runtime root: %w", err)
		}
		if rawRoot != "" {
			given, err := cliops.CanonicalRoot(rawRoot)
			if err != nil {
				return err
			}
			if given != root {
				return errors.New("mcp: runtime data root mismatch")
			}
		}
		prefix = "/v1/mcp/"
		client = cliops.LocalHTTPClient(0)
	} else {
		client, base, err = localOwnerClient(ctx, command.HTTP)
		if err != nil {
			return err
		}
		if rawRoot != "" {
			root, err = cliops.CanonicalRoot(rawRoot)
			if err != nil {
				client.CloseIdleConnections()
				return err
			}
		}
		prefix = "/api/bots/" + url.PathEscape(command.Bot) + "/mcp/"
	}
	defer client.CloseIdleConnections()
	request, err := cliops.MCPHTTPRequest(ctx, base, prefix, root, command, arguments)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("mcp: runtime request: %w", err)
	}
	reply, err := cliops.ReadMCPReply(response)
	if err != nil {
		return err
	}
	if command.JSON {
		err = json.NewEncoder(out).Encode(reply)
	} else {
		if len(reply.Data) > 0 {
			_, err = fmt.Fprintln(out, string(reply.Data))
		}
		for _, content := range reply.Content {
			if content.Text != "" {
				_, err = fmt.Fprintln(out, content.Text)
			}
			if content.Preview != "" {
				_, err = fmt.Fprintln(out, content.Preview)
			}
			if content.URI != "" {
				_, err = fmt.Fprintln(out, content.URI)
			}
			if content.File != "" {
				_, err = fmt.Fprintf(out, "%s (access: %s)\n", content.File, content.Access)
			}
		}
		for _, artifact := range []*cliops.MCPArtifact{reply.Raw, reply.Structured, reply.Artifact} {
			if artifact != nil {
				_, err = fmt.Fprintf(out, "%s (access: %s)\n", artifact.File, artifact.Access)
			}
		}
	}
	if err != nil {
		return err
	}
	if reply.IsError {
		return errors.New("mcp: remote tool reported isError; result content was preserved")
	}
	if len(reply.ProjectionErrors) > 0 {
		return errors.New("mcp: result projection failed; available content was preserved")
	}
	return nil
}

func runMCPOffline(rawRoot string, c cliops.MCPCommand, out io.Writer) error {
	root, err := resolveRoot(rawRoot)
	if err != nil {
		return err
	}
	b, err := bot.Load(root, c.Bot)
	if err != nil {
		return err
	}
	names := config.KeyEnvNames(root)
	options := mcp.Options{IsProviderKey: func(name string) bool { return config.ProviderKey(name) || slices.Contains(names, name) }}
	switch c.Action {
	case "list":
		specs, err := mcp.LoadMCP(b.MCPPath(), options)
		if err != nil {
			return err
		}
		if c.JSON {
			return json.NewEncoder(out).Encode(map[string]any{"servers": specs, "scope": "offline"})
		}
		for _, s := range specs {
			where := s.Command
			if s.URL != "" {
				where = s.URL
			}
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", s.Name, where, strings.Join(s.Args, " "), strings.Join(s.SecretEnvs, ",")); err != nil {
				return err
			}
		}
	case "add":
		spec := mcp.Spec{Name: c.Server, Command: c.Command, URL: c.URL, Args: c.CommandArgs, SecretEnvs: c.SecretEnvs}
		if err := mcp.AddMCP(b.MCPPath(), spec, options); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "added %s on %s (offline configuration)\n", c.Server, c.Bot)
		return err
	case "remove":
		if err := mcp.RemoveMCP(b.MCPPath(), c.Server, options); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "removed %s from %s (offline configuration)\n", c.Server, c.Bot)
		return err
	}
	return nil
}
