package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xbot/internal/agent"
	"xbot/internal/notable"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// ToolJobReport reuses the job's existing output file and paste directory.
// Both a local kernel and a loopback runner publish exactly this metadata
// before announcing completion; Notified is left for the receiving inbox.
func ToolJobReport(workspace string, job tools.Job, res tools.CallResult) (tools.Job, error) {
	text := res.Text
	if text == "" && res.Err != nil {
		text = "error: " + res.Err.Error()
	}
	line := notable.Line(job.Notable)
	if line != "" {
		text += "\n\n" + line
	}
	preview, err := PreviewResult(job.Output, text, line)
	if err != nil {
		return job, err
	}
	job.Result = fmt.Sprintf("工具 %s（%s）已结束：%s\n%s", job.Tool, job.ID, job.Status, preview)
	total := 0
	for _, img := range res.Images {
		if len(job.Images) >= work.PastedMaxPerMessage || total+len(img.Data) > work.PastedMaxTotalBytes {
			break
		}
		name, err := work.PastedName(img.MIME)
		if err == nil {
			err = work.WritePasted(workspace, name, img.Data)
		}
		if err != nil {
			return job, err
		}
		job.Images = append(job.Images, name)
		total += len(img.Data)
	}
	return job, nil
}

func (j *job) runToolCall(ctx context.Context, t tools.Tool, args json.RawMessage, run func(context.Context) tools.CallResult) tools.CallResult {
	if j.parent.RunTool != nil {
		return j.parent.RunTool(j.request, ctx, t, args, run)
	}
	if j.parent.JobsFor == nil {
		return run(ctx)
	}
	jobs, err := j.parent.JobsFor(j.request)
	if err != nil {
		return tools.CallResult{Err: err}
	}
	wait, bg := tools.CallWait(t, args)
	summary := agent.ToolSummary(ctx)
	return jobs.RunCall(ctx, tools.CallRequest{Name: t.Name(), Command: summary, Dir: j.workspace, Wait: wait, Background: bg, Run: run,
		Finalize: func(job tools.Job, res tools.CallResult) (tools.Job, error) {
			return ToolJobReport(j.workspace, job, res)
		},
		OnDone: func(job tools.Job) {
			j.parent.emit(Event{ID: "shelljob_" + job.ID, AgentID: j.id, Kind: tools.JobEventType, Text: job.Result, Images: job.Images})
		},
	})
}

// Tool job controls reuse the existing runner control transport.
func (r *Remote) CancelToolJob(ctx context.Context, dir, id string) error {
	if !strings.HasPrefix(id, "sh_") {
		return fmt.Errorf("bad tool job id")
	}
	return r.post(ctx, "/v1/message", Control{ID: id, Kind: KindCancel, JobDir: dir})
}
