package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/tools"
)

// JobsHandlers keeps the three original shell job methods and their own IO
// routes. All three ignore calling context and have no turn requirement; the
// process-lifetime jobs table, live writers, owned cancels and the Remote
// fallback only exist on this instance, so the endpoint is the source.
func (k *Kernel) JobsHandlers() *cliops.JobsHandlers {
	redact := k.redactor()
	fits := func(reply cliops.JobsReply) bool {
		raw, err := json.Marshal(reply)
		return err == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(cliops.FormatJobs(reply)) <= tools.DefaultShellOutputBytes
	}
	fail := func(err error) (cliops.JobsReply, error) {
		reply := cliops.JobsReply{Error: scrubSecrets(redact(err.Error()), k.secretValues())}
		for !fits(reply) {
			reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
		}
		return reply, errors.New(reply.Error)
	}
	finish := func(text string, err error) (cliops.JobsReply, error) {
		if err != nil {
			return fail(err)
		}
		text = config.RedactHeld(text)
		if k.Resources != nil && k.Resources.Redact != nil {
			text = k.Resources.Redact(text)
		}
		reply := cliops.JobsReply{Text: text}
		if fits(reply) {
			return reply, nil
		}
		return fail(errors.New("jobs: reply exceeds 32 KiB Shell output budget"))
	}
	return &cliops.JobsHandlers{
		Output: func(_ context.Context, in cliops.JobsOutput) (cliops.JobsReply, error) {
			release := k.holdOperations()
			defer release()
			jobs := k.shellJobs()
			if jobs == nil {
				return finish("", errNoShellJobs)
			}
			return finish(jobs.Output(strings.TrimSpace(in.ID), in.Offset, in.Tail))
		},
		Status: func(_ context.Context, in cliops.JobsStatus) (cliops.JobsReply, error) {
			release := k.holdOperations()
			defer release()
			jobs := k.shellJobs()
			if jobs == nil {
				return finish("", errNoShellJobs)
			}
			id := strings.TrimSpace(in.ID)
			if id == "" {
				list := jobs.List()
				if len(list) == 0 {
					return finish("no background tool jobs", nil)
				}
				var b strings.Builder
				for i, job := range list {
					if i >= 32 {
						b.WriteString("...\n")
						break
					}
					fmt.Fprintf(&b, "%s %s %s", job.ID, job.Status, clipRunes(job.Command, 80))
					if job.Cwd != "" {
						fmt.Fprintf(&b, " cwd %s", job.Cwd)
					}
					if job.Held > 0 {
						fmt.Fprintf(&b, " held_bytes: %d", job.Held)
					}
					b.WriteByte('\n')
				}
				return finish(strings.TrimRight(b.String(), "\n"), nil)
			}
			job, err := jobs.Status(id)
			if err != nil {
				return finish("", err)
			}
			line := fmt.Sprintf("%s %s", job.ID, job.Status)
			if job.Status == "done" {
				line += fmt.Sprintf(" exit %d", job.ExitCode)
			}
			if job.Cwd != "" {
				line += "\ncwd: " + job.Cwd
			}
			if job.Held > 0 {
				line += fmt.Sprintf("\nheld_bytes: %d", job.Held)
			}
			if job.Output != "" {
				line += "\noutput: " + job.Output
			}
			return finish(line, nil)
		},
		Cancel: func(_ context.Context, in cliops.JobsCancel) (cliops.JobsReply, error) {
			release := k.holdOperations()
			defer release()
			jobs := k.shellJobs()
			if jobs == nil {
				return finish("", errNoShellJobs)
			}
			id := strings.TrimSpace(in.ID)
			out, err := jobs.Cancel(id)
			if errors.Is(err, tools.ErrForeignJob) {
				if remote, ok := k.execSnapshot().(*exec.Remote); ok {
					err = remote.CancelToolJob(context.Background(), jobs.Dir, id)
					if err == nil {
						out = "cancellation requested " + id
					}
				}
			}
			return finish(out, err)
		},
	}
}
