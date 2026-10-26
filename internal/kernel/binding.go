package kernel

import (
	"context"
	"errors"
	"fmt"

	"xbot/internal/compact"
	"xbot/internal/provider"
	"xbot/prompts"
)

// PrepareBinding is called under the bot handle lock, before SetBinding.
// It never mutates the live binding or Budget: old-provider background jobs
// may still finish while the new request budget is being checked.
func (k *Kernel) PrepareBinding(ctx context.Context, providerName, modelName string, window int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := k.CheckBindingIdle(); err != nil {
		return err
	}
	target, ceiling, smaller := k.bindingLines(window)
	if !smaller {
		return nil
	}
	// Add the whole new overlay as conservative headroom; the current overlay
	// is already counted. Tool schema and a fresh environment are reserved too.
	overlay, err := prompts.Load(k.Bot.Root, providerName, modelName)
	if err != nil {
		return err
	}
	list, err := k.modelTools()
	if err != nil {
		return err
	}
	toolTokens := 0
	for _, tool := range list {
		toolTokens += estimateTools([]provider.ToolDef{{Name: tool.Name(), Description: tool.Description(), Parameters: tool.Parameters()}})
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	st, _, err = k.applyReadyJob(migrate(st))
	if err != nil {
		return err
	}
	size := func() (int, error) {
		_, usage, err := k.assemble(st, st.MemSnap)
		if err != nil {
			return 0, err
		}
		prev, err := k.visibleEnvironment(st)
		if err != nil {
			return 0, err
		}
		next, err := k.sampleEnvironment(prev)
		if err != nil {
			return 0, err
		}
		snapshot := environmentDelta(prev, next, environmentBasis(st, nil), st.MemSnap)
		// Leave bounded headroom for the incoming message and hosted tools.
		return k.scaled(usage.Total()+toolTokens+compact.Estimate(overlay)+compact.Estimate(snapshot)) + min(1024, ceiling/20), nil
	}
	before, err := size()
	if err != nil {
		return err
	}
	if before <= target {
		return nil
	}
	reject := func(cause error) error {
		return fmt.Errorf("上下文仍超新窗口，需先删分支/导出（绑定未修改；目标 %d，估算 %d）：%w", target, before, cause)
	}
	from, to, _, ok, err := k.compactPlanBudget(st, 0, target/tailShare)
	if err != nil {
		return reject(err)
	}
	if !ok {
		if before <= ceiling {
			return nil
		}
		return reject(errors.New("没有可安全压缩的范围"))
	}
	if k.Compactor == nil {
		return reject(errors.New("未配置压缩器"))
	}
	if _, err := k.CompactRange(ctx, from, to, "binding: "+defaultFocus); err != nil {
		return reject(err)
	}
	st, err = k.Bot.LoadState()
	if err != nil {
		return err
	}
	if err := k.ensureSnapshot(st); err != nil {
		return err
	}
	before, err = size()
	if err != nil {
		return err
	}
	if before > ceiling {
		return reject(errors.New("压缩后仍不能承载"))
	}
	return ctx.Err()
}

// BindingShrinksWindow lets a cold web cache avoid constructing an old client
// just to replace expired credentials with a same-size or larger binding.
func (k *Kernel) BindingShrinksWindow(window int) bool {
	_, _, smaller := k.bindingLines(window)
	return smaller
}

func (k *Kernel) bindingLines(window int) (target, ceiling int, smaller bool) {
	if limit := k.Bot.Config().ContextLimit; limit > 0 && (window <= 0 || limit < window) {
		window = limit
	}
	target, ceiling = compact.Lines(window)
	oldTarget, oldCeiling := k.lines()
	return target, ceiling, target < oldTarget || ceiling < oldCeiling
}
