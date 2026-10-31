package kernel

import (
	"context"

	"xbot/internal/bot"
	"xbot/internal/envprompt"
	"xbot/internal/promptinject"
	"xbot/internal/provider"
)

func (k *Kernel) registeredEnvironment() *envprompt.Env {
	k.promptOnce.Do(func() {
		k.promptEnv.Register(envprompt.Field{
			Key:    "time",
			Sample: func(context.Context) (envprompt.Value, error) { return k.localStamp(), nil },
			Prompt: func(v envprompt.Value, _ envprompt.Change) string {
				return "当前时间：" + v.(string) + "。本条取代先前的时间公告；查时间以当前时钟为准。"
			},
		})
	})
	return &k.promptEnv
}

// Environment is the first tail prompt source. The legacy fields remain in
// their existing replay path until they acquire their own registered Prompt.
// Preview and publication share this render path; only publication commits.
func (k *Kernel) environmentPrompts(ctx context.Context, legacy string, full bool) ([]string, func(), error) {
	var sources promptinject.Registry
	var commit func()
	sources.Register(promptinject.Source{Name: "environment", Order: 0, Render: func(ctx context.Context) (string, error) {
		text, done, err := k.registeredEnvironment().Prepare(ctx, full)
		commit = done
		if text != "" {
			text = bot.FrameEnvironment(text)
		}
		return text, err
	}})
	texts, err := sources.Render(ctx)
	if err != nil {
		return nil, nil, err
	}
	if legacy != "" {
		texts = append(texts, legacy)
	}
	return texts, commit, nil
}

func (k *Kernel) appendEnvironmentPrompts(ctx context.Context, legacy string, full bool) ([]provider.Message, error) {
	texts, commit, err := k.environmentPrompts(ctx, legacy, full)
	if err != nil {
		return nil, err
	}
	var out []provider.Message
	for _, text := range texts {
		m, err := k.appendEnvironment(text)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	commit()
	return out, nil
}
