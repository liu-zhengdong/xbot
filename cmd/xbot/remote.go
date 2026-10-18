package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"xbot/internal/gateway"
	"xbot/internal/remote"
)

func cmdRemote(root string, args []string) error {
	usage := errors.New("usage: xbot remote id | gateway [URL|default] | add <name> <key> [--xkey KEY] [--person ID] | invite [--person ID] | join <code> [--name NAME] [--as NAME] [--person ID] | list | remove <name>")
	if len(args) == 0 {
		return usage
	}
	switch {
	case args[0] == "id" && len(args) == 1:
		_, id, key, err := remote.Identity(root)
		if err != nil {
			return err
		}
		_, xkey, err := remote.XIdentity(root)
		if err != nil {
			return err
		}
		fmt.Printf("id %s\nkey %s\nxkey %s\n", id, key, xkey)
	case args[0] == "gateway" && len(args) == 2:
		return remote.SetGateway(root, args[1])
	case (args[0] == "gateway" || args[0] == "list") && len(args) == 1:
		cfg, err := remote.Load(root)
		if err != nil {
			return err
		}
		if args[0] == "gateway" {
			suffix := ""
			if cfg.Gateway == "" {
				suffix = " (default)"
			}
			fmt.Println(cfg.GatewayURL() + suffix)
		} else {
			for _, d := range cfg.Devices {
				fmt.Printf("%s\t%s\t%s\n", d.Name, d.ID, d.Person)
			}
		}
	case args[0] == "add" || args[0] == "invite" || args[0] == "join":
		ordered, err := reorderFlags(args[1:], map[string]bool{"person": true, "xkey": true, "name": true, "as": true})
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("remote "+args[0], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		person := fs.String("person", "", "local person id")
		var name, as, xkey string
		if args[0] == "join" {
			fs.StringVar(&name, "name", "", "name for the inviting device")
			fs.StringVar(&as, "as", "", "name the inviting device uses for this one")
		}
		if args[0] == "add" {
			fs.StringVar(&xkey, "xkey", "", "device x25519 public key")
		}
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		switch {
		case args[0] == "add" && fs.NArg() == 2:
			_, err = remote.AddDevice(root, fs.Arg(0), fs.Arg(1), xkey, *person)
		case args[0] == "invite" && fs.NArg() == 0:
			var code, id string
			code, err = remote.CreateInvite(root, *person, time.Now())
			if err == nil {
				var pc remote.PairCode
				pc, err = remote.ParseCode(code)
				if err != nil {
					return err
				}
				key, self, _, keyErr := remote.Identity(root)
				id, err = self, keyErr
				if err == nil {
					err = (&gateway.Client{Base: pc.Gateway, Key: key}).Register(context.Background(), pc.Token)
				}
			}
			if err == nil {
				fmt.Printf("%s\nfingerprint %s expires in 10 minutes; the code contains the gateway join token — share it only with your own device\n", code, id[:8])
			}
		case args[0] == "join" && fs.NArg() == 1:
			var d remote.Device
			d, err = remote.Join(context.Background(), root, fs.Arg(0), name, as, *person, time.Now())
			if err == nil {
				fmt.Printf("paired with %s fingerprint %s\nThe other side completes pairing when its serve is running.\n", d.Name, d.ID[:8])
			}
		default:
			return usage
		}
		return err
	case args[0] == "remove" && len(args) == 2:
		return remote.RemoveDevice(root, args[1])
	default:
		return usage
	}
	return nil
}
