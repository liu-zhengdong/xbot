package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"xbot/internal/upgrade"
	"xbot/internal/version"
)

func cmdVersion() error {
	fmt.Println(version.Version)
	return nil
}

func cmdUpgrade(root string, args []string) error {
	ordered, err := reorderFlags(args, nil)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "only report whether a newer release exists")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot upgrade [--check]")
	}
	ctx := context.Background()
	// The manual command forces a dev build to see releases; the periodic
	// checker never does.
	info, err := upgrade.CheckForce(ctx, version.Version, true)
	if err != nil {
		return err
	}
	fmt.Printf("current: %s\nlatest:  %s\n", version.Version, info.Latest)
	if *check {
		return nil
	}
	if info.Downgrade {
		fmt.Println("latest release is older; refusing to downgrade")
		return nil
	}
	if !info.Available {
		fmt.Println("already up to date")
		return nil
	}
	path, err := upgrade.Apply(ctx, info)
	if err != nil {
		return err
	}
	fmt.Printf("updated %s to %s\n", path, info.Latest)
	return nil
}
