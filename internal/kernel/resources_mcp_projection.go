package kernel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/work"
)

const mcpPreviewBytes = 2 << 10

func (r *BotResources) mcpRedact(text string) string {
	if r.Redact != nil {
		return r.Redact(text)
	}
	return text
}

func (r *BotResources) mcpFailure(err error) error {
	if err == nil {
		return nil
	}
	text := r.mcpRedact(err.Error())
	if text == err.Error() {
		return err
	}
	return errors.New(text)
}

// Decode with UseNumber so redaction cannot round integer arguments/results.
func (r *BotResources) redactMCPJSON(raw []byte) (json.RawMessage, error) {
	return redactRuntimeJSON(raw, r.mcpRedact)
}

func redactRuntimeJSON(raw []byte, scrub func(string) string) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var redact func(any) any
	redact = func(value any) any {
		switch v := value.(type) {
		case string:
			return scrub(v)
		case []any:
			for i := range v {
				v[i] = redact(v[i])
			}
		case map[string]any:
			for key, item := range v {
				v[key] = redact(item)
			}
		}
		return value
	}
	return json.Marshal(redact(value))
}

func (r *BotResources) saveMCPResult(data []byte, ext string) (*cliops.MCPArtifact, error) {
	r.route.RLock()
	if r.closed || r.ctx.Err() != nil {
		r.route.RUnlock()
		return nil, errResourcesUnavailable
	}
	path, err := work.WriteResult(work.BotDir(r.current.Root, r.current.Name()), data, ext)
	r.route.RUnlock()
	if err != nil {
		return nil, err
	}
	mime := "application/octet-stream"
	if ext == "json" {
		mime = "application/json"
	} else if ext == "txt" {
		mime = "text/plain"
	}
	return &cliops.MCPArtifact{Type: ext, MIMEType: mime, File: path, Access: "local"}, nil
}

func (r *BotResources) saveMCPImage(data []byte) (*cliops.MCPArtifact, error) {
	mime := http.DetectContentType(data)
	r.route.RLock()
	if r.closed || r.ctx.Err() != nil {
		r.route.RUnlock()
		return nil, errResourcesUnavailable
	}
	path, err := work.WriteGenerated(work.BotDir(r.current.Root, r.current.Name()), data)
	r.route.RUnlock()
	if err != nil {
		return nil, err
	}
	return &cliops.MCPArtifact{Type: "image", MIMEType: mime, File: path, Access: "local"}, nil
}

// boundMCPReply checks the final serialized payload, including escaped text and
// metadata. Large projections keep their complete manifest in the workspace.
func (r *BotResources) boundMCPReply(reply cliops.MCPReply) (cliops.MCPReply, error) {
	raw, err := json.Marshal(reply)
	if err != nil {
		return cliops.MCPReply{}, err
	}
	if len(raw)+1 <= cliops.MCPMaxReply { // Encoder's final newline counts too.
		return reply, nil
	}
	artifact, saveErr := r.saveMCPResult(raw, "json")
	small := cliops.MCPReply{Raw: reply.Raw, Structured: reply.Structured, Artifact: artifact, IsError: reply.IsError, Saved: reply.Saved}
	small.RuntimeError, _ = clip.Bytes(reply.RuntimeError, mcpPreviewBytes)
	small.Content = []cliops.MCPContent{{Type: "text", Preview: "Complete MCP response is stored in the local artifact."}}
	if len(reply.ProjectionErrors) != 0 {
		message, _ := clip.Bytes(reply.ProjectionErrors[0], mcpPreviewBytes)
		small.ProjectionErrors = []string{fmt.Sprintf("%d projection error(s); first: %s", len(reply.ProjectionErrors), message)}
	}
	if saveErr != nil {
		small.ProjectionErrors = append(small.ProjectionErrors, "save projection manifest: "+r.mcpRedact(saveErr.Error()))
	}
	return small, nil
}

func (r *BotResources) projectMCPCall(raw json.RawMessage) (cliops.MCPReply, error) {
	raw, err := r.redactMCPJSON(raw)
	if err != nil {
		return cliops.MCPReply{ProjectionErrors: []string{"decode MCP result: " + r.mcpRedact(err.Error())}}, nil
	}
	reply := cliops.MCPReply{}
	reply.Raw, err = r.saveMCPResult(raw, "json")
	if err != nil {
		reply.ProjectionErrors = append(reply.ProjectionErrors, "save raw result: "+r.mcpRedact(err.Error()))
	}
	var result struct {
		Content    []json.RawMessage `json:"content"`
		Structured json.RawMessage   `json:"structuredContent"`
		IsError    bool              `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		reply.ProjectionErrors = append(reply.ProjectionErrors, "decode MCP content: "+r.mcpRedact(err.Error()))
		return r.boundMCPReply(reply)
	}
	reply.IsError = result.IsError
	if len(result.Structured) != 0 {
		reply.Structured, err = r.saveMCPResult(result.Structured, "json")
		if err != nil {
			reply.ProjectionErrors = append(reply.ProjectionErrors, "save structured content: "+r.mcpRedact(err.Error()))
		}
	}
	for _, block := range result.Content {
		content := r.projectMCPContent(block)
		reply.Content = append(reply.Content, content)
		if content.ProjectionError != "" {
			reply.ProjectionErrors = append(reply.ProjectionErrors, content.ProjectionError)
		}
	}
	return r.boundMCPReply(reply)
}

func (r *BotResources) projectMCPContent(raw json.RawMessage) cliops.MCPContent {
	var block struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Data     string          `json:"data"`
		MIMEType string          `json:"mimeType"`
		URI      string          `json:"uri"`
		Resource json.RawMessage `json:"resource"`
	}
	parseErr := json.Unmarshal(raw, &block)
	content := cliops.MCPContent{Type: block.Type, MIMEType: block.MIMEType}
	if content.Type == "" {
		content.Type = "unknown"
	}
	var artifact *cliops.MCPArtifact
	var err error
	if parseErr != nil {
		err = parseErr
	} else {
		switch block.Type {
		case "text":
			if len(block.Text) <= mcpPreviewBytes {
				content.Text = block.Text
				return content
			}
			content.Preview, _ = clip.Bytes(block.Text, mcpPreviewBytes)
			artifact, err = r.saveMCPResult([]byte(block.Text), "txt")
		case "image":
			var data []byte
			data, err = base64.StdEncoding.DecodeString(block.Data)
			if err == nil {
				artifact, err = r.saveMCPImage(data)
			}
		case "resource_link":
			content.URI = block.URI
			return content
		case "resource", "embedded_resource":
			var resource struct {
				URI      string  `json:"uri"`
				MIMEType string  `json:"mimeType"`
				Text     *string `json:"text"`
				Blob     *string `json:"blob"`
			}
			err = json.Unmarshal(block.Resource, &resource)
			content.URI, content.MIMEType = resource.URI, resource.MIMEType
			if err == nil {
				switch {
				case resource.Text != nil:
					content.Preview, _ = clip.Bytes(*resource.Text, mcpPreviewBytes)
					artifact, err = r.saveMCPResult([]byte(*resource.Text), "txt")
				case resource.Blob != nil:
					var data []byte
					data, err = base64.StdEncoding.DecodeString(*resource.Blob)
					if err == nil {
						artifact, err = r.saveMCPResult(data, "bin")
					}
				default:
					artifact, err = r.saveMCPResult(raw, "json")
				}
			}
		default:
			artifact, err = r.saveMCPResult(raw, "json")
		}
	}
	if err != nil {
		content.ProjectionError = r.mcpRedact(block.Type + " projection: " + err.Error())
		// Even a malformed known image/resource remains available in its
		// original JSON block instead of silently dropping the remote content.
		var fallbackErr error
		artifact, fallbackErr = r.saveMCPResult(raw, "json")
		if fallbackErr != nil {
			content.ProjectionError = r.mcpRedact(errors.Join(err, fallbackErr).Error())
		}
	}
	if artifact != nil {
		content.File, content.Access = artifact.File, artifact.Access
		if content.MIMEType == "" || block.Type == "image" {
			content.MIMEType = artifact.MIMEType
		}
	}
	return content
}
