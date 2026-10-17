package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cron"
	"xbot/internal/link"
)

func cmdRoutine(root string, args []string) error {
	if len(args) == 0 || args[0] != "migrate" {
		return errors.New("usage: xbot routine migrate [--dry-run] [--bot NAME]")
	}
	ordered, err := reorderFlags(args[1:], map[string]bool{"bot": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("routine migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", false, "report changes without writing")
	only := fs.String("bot", "", "only this bot")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot routine migrate [--dry-run] [--bot NAME]")
	}
	cfgs, err := bot.List(root)
	if err != nil {
		return err
	}
	if *only != "" {
		found := false
		for _, cfg := range cfgs {
			if cfg.Name == *only {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("routine migrate: unknown bot %q", *only)
		}
	}
	now, loc := time.Now(), time.Local
	changed, failed := 0, 0
	fmt.Printf("%-12s %-12s %-18s %-8s %-18s %-8s %-25s | %-25s | %s\n",
		"BOT", "ID", "OLD CRON", "OLD ZONE", "NEW CRON", "NEW ZONE", "NEXT BEFORE", "NEXT AFTER", "NOTE")
	for _, cfg := range cfgs {
		if *only != "" && cfg.Name != *only {
			continue
		}
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xbot: routine migrate: %s: %v\n", cfg.Name, err)
			failed++
			continue
		}
		migs, err := link.MigrateRoutines(b.RoutineDir(), loc, now, *dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xbot: routine migrate: %s: %v\n", cfg.Name, err)
			failed++
			continue
		}
		for _, m := range migs {
			if m.Changed {
				changed++
			}
			before, after := "-", "-"
			if m.OldCron != "" {
				before = nextThree(m.OldCron, zoneLoc(m.OldZone, loc), now)
				after = nextThree(m.NewCron, zoneLoc(m.NewZone, loc), now)
			}
			fmt.Printf("%-12s %-12s %-18s %-8s %-18s %-8s %-25s | %-25s | %s\n",
				cfg.Name, m.ID, dash(m.OldCron), zoneOrUTC(m.OldZone), dash(m.NewCron), zoneOrUTC(m.NewZone), before, after, m.Note)
		}
	}
	if *dryRun {
		fmt.Printf("dry run: %d routine(s) would change\n", changed)
	} else if changed > 0 {
		fmt.Printf("%d routine(s) changed\n", changed)
	}
	if failed > 0 {
		return fmt.Errorf("routine migrate: %d bot(s) failed", failed)
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func zoneOrUTC(zone string) string {
	if zone == "" {
		return "UTC"
	}
	return zone
}

func zoneLoc(zone string, loc *time.Location) *time.Location {
	if zone == "local" {
		return loc
	}
	return time.UTC
}

func nextThree(expr string, loc *time.Location, now time.Time) string {
	s, err := cron.Parse(expr)
	if err != nil {
		return "-"
	}
	out := ""
	for i := 0; i < 3; i++ {
		next, ok := s.Next(now, loc)
		if !ok {
			break
		}
		if out != "" {
			out += " "
		}
		out += next.In(loc).Format(time.RFC3339)
		now = next
	}
	if out == "" {
		return "-"
	}
	return out
}
