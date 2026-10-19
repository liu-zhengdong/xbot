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
	if info.Downgrade {
		fmt.Println("latest release is older; refusing to downgrade")
		return nil
	}
	if !info.Available {
		fmt.Println("already up to date")
		return nil
	}
	if upgrade.AssetName(info.Latest) == "" {
		if asset := upgrade.DownloadAsset(info.Latest); asset != "" && info.AssetURL(asset) != "" {
			fmt.Println("this build cannot replace itself; download and run:")
			fmt.Println(info.AssetURL(asset))
		} else {
			fmt.Println("this build has no update package; see", info.URL)
		}
		return nil
	}
	if *check {
		return nil
	}
	staged, err := upgrade.Stage(ctx, info, root, progressPrinter())
	if err != nil {
		return err
	}
	fmt.Println()
	if err := upgrade.Install(staged); err != nil {
		return err
	}
	upgrade.ClearStaged(root)
	fmt.Printf("installed %s; restart xbot to use it\n", info.Latest)
	return nil
}

// progressPrinter reports a download as whole percents on one line.
func progressPrinter() func(done, total int64) {
	last := int64(-1)
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		pct := done * 100 / total
		if pct == last {
			return
		}
		last = pct
		fmt.Printf("\rdownloading %d%%", pct)
	}
}
