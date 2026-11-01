package kernel

import (
	"context"
	"errors"
	"path/filepath"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/exec"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/peer"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

var errResourcesUnavailable = errors.New("bot resources paused or closed")

// CurrentBot resolves a retained source or wakeup against the current route.
// Temporary deletion pauses do not stop original completion/notification delivery.
func (r *BotResources) CurrentBot() (*bot.Bot, error) {
	r.route.RLock()
	defer r.route.RUnlock()
	if r.closed || r.ctx.Err() != nil {
		return nil, errResourcesUnavailable
	}
	return bot.Load(r.current.Root, r.current.Name())
}

// TryCurrentHandle keeps the current-path check and handle-lock creation in one
// short route read. TryLockHandle can create its parent directory, so merely
// returning CurrentBot before locking would let a stale wake recreate old paths.
func (r *BotResources) TryCurrentHandle() (*bot.Bot, *fsutil.FileLock, bool, error) {
	if !r.Jobs.AdmissionOpen() {
		return nil, nil, false, errResourcesUnavailable
	}
	r.route.RLock()
	defer r.route.RUnlock()
	if r.closed || r.ctx.Err() != nil {
		return nil, nil, false, errResourcesUnavailable
	}
	b, err := bot.Load(r.current.Root, r.current.Name())
	if err != nil {
		return nil, nil, false, err
	}
	handle, ok, err := b.TryLockHandle()
	return b, handle, ok, err
}

// Name reports the route's name even after its final close fence, allowing the
// host to keep its index consistent after a move with a secondary error.
func (r *BotResources) Name() string {
	r.route.RLock()
	defer r.route.RUnlock()
	return r.current.Name()
}

// AdmissionOpen is checked before constructing a replacement kernel or moving
// resources. Jobs is always consulted outside route (delivery takes Jobs first).
func (r *BotResources) AdmissionOpen() bool {
	if !r.Jobs.AdmissionOpen() {
		return false
	}
	_, err := r.CurrentBot()
	return err == nil
}

// MoveResources runs the existing coding, accepted Local and Jobs movers.
// Source operations and the host name index (when present) are already held.
func (k *Kernel) MoveResources(change bot.ProfileChange) (bot.ProfileChangeResult, error) {
	b := k.Bot
	if k.Resources != nil {
		var err error
		b, err = k.Resources.CurrentBot()
		if err != nil {
			return bot.ProfileChangeResult{}, err
		}
	}
	previous, next := b.Name(), b.Name()
	if change.Name != nil {
		next = *change.Name
	}
	var coding *code.Link
	k.linksMu.Lock()
	for _, item := range k.Links {
		if c, ok := item.(*code.Link); ok {
			coding = c
			break
		}
	}
	k.linksMu.Unlock()
	var result bot.ProfileChangeResult
	var after error
	move := func() error {
		moved, err := exec.MoveBot(b.AgentsDir(), filepath.Join(bot.Dir(b.Root, next), "agents"), func() (bool, error) {
			var err error
			result, err = moveBotResources(k.Resources, k, change)
			return result.Moved, err
		})
		if moved {
			after = err
			return nil
		}
		return err
	}
	var err error
	if coding == nil {
		err = move()
	} else {
		dir := bot.Dir(b.Root, next)
		err = coding.Move(dir, filepath.Join(dir, "inbox.jsonl"), func(text string) error {
			return k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
				return current.Append(bot.Entry{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}})
			})
		}, move)
	}
	// Every mover has returned: no route/storage/owners lock is held here.
	if result.Moved {
		rekeyLiveTurn(b.Root, previous, result.Name, k.Resources)
	}
	return result, errors.Join(err, after)
}

// RenameTo remains the name-only, resource-only cold host entry.
func (r *BotResources) RenameTo(next string) error {
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: &next})
	if err != nil {
		return err
	}
	_, err = r.ApplyProfileChange(change)
	return err
}

// ApplyProfileChange is the cold resource path; it never constructs a Kernel.
// The change has already passed boundary normalization.
func (r *BotResources) ApplyProfileChange(change bot.ProfileChange) (result bot.ProfileChangeResult, err error) {
	b, err := r.CurrentBot()
	if err != nil {
		return result, err
	}
	if change.Name == nil || *change.Name == b.Name() {
		err = r.WithCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
			_, result, err = current.ApplyProfileChange(change)
			return err
		})
		return result, err
	}
	_, err = exec.MoveBot(b.AgentsDir(), filepath.Join(bot.Dir(b.Root, *change.Name), "agents"), func() (bool, error) {
		result, err = moveBotResources(r, nil, change)
		return result.Moved, err
	})
	if result.Moved {
		rekeyLiveTurn(b.Root, b.Name(), result.Name, r)
	}
	return result, err
}

func moveBotResources(r *BotResources, source *Kernel, change bot.ProfileChange) (result bot.ProfileChangeResult, err error) {
	var b *bot.Bot
	var jobs *tools.Jobs
	if r != nil {
		if !r.AdmissionOpen() {
			return result, errResourcesUnavailable
		}
		b, err = r.CurrentBot()
		if err != nil {
			return result, err
		}
		jobs = r.Jobs
	} else {
		b, jobs = source.Bot, source.shellJobs()
	}
	next := b.Name()
	if change.Name != nil {
		next = *change.Name
	}
	var after error
	err = jobs.Move(filepath.Join(bot.Dir(b.Root, next), "shell-jobs"), func() error {
		if r != nil {
			r.route.Lock()
			defer r.route.Unlock()
			if r.closed || r.ctx.Err() != nil || r.current.Name() != b.Name() {
				return errResourcesUnavailable
			}
		}
		current, receipt, applyErr := b.ApplyProfileChange(change)
		result = receipt
		if !receipt.Moved {
			return applyErr
		}
		// Peer files are changed only after primary-locked target validation
		// and an actual move. Their later errors are a truthful partial result.
		after = errors.Join(applyErr, peer.RenameMember(b.Root, b.Name(), receipt.Name))
		box, boxErr := inbox.Open(current.InboxPath())
		after = errors.Join(after, boxErr)
		if r != nil {
			r.current, r.box = current, box
			if boxErr != nil {
				r.closed = true
			}
			if r.MCP != nil {
				after = errors.Join(after, r.manager.Rekey(r.MCP, receipt.Name))
			}
		} else if source != nil {
			source.Bot, source.Inbox = current, box
		}
		return nil
	})
	return result, errors.Join(err, after)
}

// BeginDelete pauses new tool admission, keeping the same Jobs/ref and inbound
// route available. The host serializes this short step with rename and lookup.
func (r *BotResources) BeginDelete(name string) error {
	if !r.AdmissionOpen() {
		return errResourcesUnavailable
	}
	b, err := r.CurrentBot()
	if err != nil {
		return err
	}
	if b.Name() != name {
		return errors.New("bot delete: target changed")
	}
	if err := r.Jobs.PauseAdmission(); err != nil {
		return err
	}
	if r.MCP != nil {
		if err := r.MCP.Pause(); err != nil {
			_ = r.Jobs.ResumeAdmission()
			return err
		}
	}
	return nil
}

// ResumeAdmission is used only after owned calls have actually joined and the
// physical target still exists. Permanent host closure can never be reversed.
func (r *BotResources) ResumeAdmission() error {
	r.route.RLock()
	if r.closed || r.ctx.Err() != nil {
		r.route.RUnlock()
		return errResourcesUnavailable
	}
	_, err := bot.Load(r.current.Root, r.current.Name())
	r.route.RUnlock()
	if err != nil {
		return err
	}
	if err := r.Jobs.ResumeAdmission(); err != nil {
		return err
	}
	if r.MCP != nil {
		if err := r.MCP.Resume(); err != nil {
			_ = r.Jobs.PauseAdmission()
			return err
		}
	}
	return nil
}

// FinishDelete fences the final physical removal with Jobs metadata and route.
// Deliver writes its ack after releasing route; taking the existing same-dir
// mover prevents that final ack from recreating a directory just removed.
func (r *BotResources) FinishDelete(name string) (removed bool, err error) {
	b, err := r.CurrentBot()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(bot.Dir(b.Root, name), "shell-jobs")
	err = r.Jobs.Move(dir, func() error {
		r.route.Lock()
		defer r.route.Unlock()
		if r.closed || r.ctx.Err() != nil || r.current.Name() != name {
			return errors.New("bot delete: target changed or closed")
		}
		var deleteErr error
		removed, deleteErr = bot.DeleteWithResult(b.Root, name)
		if removed {
			r.closed = true
		}
		return deleteErr
	})
	return removed, err
}

// DeleteRuntime is the direct chat equivalent of the host's staged deletion.
func (r *BotResources) DeleteRuntime(ctx context.Context, name string) error {
	b, err := r.CurrentBot()
	if err != nil {
		return err
	}
	if err := bot.CheckDelete(b.Root, name); err != nil {
		return err
	}
	if err := r.BeginDelete(name); err != nil {
		return err
	}
	r.Jobs.Shutdown()
	if err := ctx.Err(); err != nil {
		return errors.Join(err, r.ResumeAdmission())
	}
	removed, err := r.FinishDelete(name)
	if removed {
		r.Close()
	} else {
		err = errors.Join(err, r.ResumeAdmission())
	}
	return err
}
