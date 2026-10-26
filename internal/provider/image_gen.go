package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"xbot/internal/work"
)

// ImageGen turns on the vendor-hosted image_generation tool for one request.
// It is carried on the context, not on the bound Responses, because only the
// kernel knows where the bot's workspace is and the setting is read live; a
// caller without a workspace never mounts the tool. It is a capability of its
// own: suppressing hosted search leaves it alone.
//
// Model is the OpenAI tool-level image model; empty uses the vendor default,
// and xAI never receives it. Save stores one decoded picture and returns its
// path relative to the bot workspace. Delivery is a separate notify_owner call.
type ImageGen struct {
	Model string
	Save  func([]byte) (string, error)
}

type imageGenKey struct{}

// WithImageGen mounts hosted image generation on Responses requests made with ctx.
func WithImageGen(ctx context.Context, g ImageGen) context.Context {
	return context.WithValue(ctx, imageGenKey{}, g)
}

func imageGenFrom(ctx context.Context) (ImageGen, bool) {
	g, ok := ctx.Value(imageGenKey{}).(ImageGen)
	return g, ok && g.Save != nil
}

const (
	// maxImagesPerResponse caps how many pictures one response may write.
	maxImagesPerResponse = 8
	// imageStreamLine is the longest stream line accepted while image
	// generation is mounted. response.completed may repeat all eight maximum
	// images as base64: 4*ceil(bytes/3) per image, plus the existing 1 MiB
	// stripped-output budget and 4 KiB for SSE/response framing. The scanner
	// grows on demand; other requests keep the 1 MiB line cap.
	imageStreamLine = maxImagesPerResponse*4*((work.GeneratedMaxBytes+2)/3) + (1 << 20) + (4 << 10)
)

// imageTool is the Responses tool entry. xAI picks its own Imagine model and
// takes none of OpenAI's options.
func imageTool(base string, g ImageGen) map[string]any {
	t := map[string]any{"type": "image_generation"}
	if m := strings.TrimSpace(g.Model); m != "" && !xaiEndpoint(base) {
		t["model"] = m
	}
	return t
}

// imageStore writes each image_generation_call result once per stream. The
// same item arrives in output_item.done and again in response.completed; the
// second copy reuses the first path.
type imageStore struct {
	gen   ImageGen
	ok    bool
	saved map[string]string
	fails map[string]string
}

func newImageStore(ctx context.Context) *imageStore {
	g, ok := imageGenFrom(ctx)
	return &imageStore{gen: g, ok: ok, saved: map[string]string{}, fails: map[string]string{}}
}

// strip replaces an image_generation_call's base64 result with the stored
// path (xbot_path) or the reason it was not stored (xbot_error). Other items
// pass through untouched. The bytes never reach the message record.
func (s *imageStore) strip(raw json.RawMessage, index int, final bool) (json.RawMessage, error) {
	var head struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Result string `json:"result"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Type != "image_generation_call" {
		return raw, nil
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	delete(item, "result")
	// Only this store can attest to a local save, never vendor-supplied fields.
	delete(item, "xbot_path")
	delete(item, "xbot_error")
	if !final {
		return json.Marshal(item)
	}
	// A partial payload or a cached save cannot turn a failed/incomplete
	// vendor result into success. Only a completed call can attest a result.
	if head.Status != "" && head.Status != "completed" {
		item["xbot_error"] = "厂商生图未完成 · " + head.Status
		if head.Result == "" {
			item["xbot_error"] = "厂商未返回图片 · " + head.Status
		}
		return json.Marshal(item)
	}
	key := "id:" + head.ID
	if head.ID == "" {
		key = fmt.Sprintf("index:%d", index)
	}
	path, fail := s.save(key, head.Result)
	if path != "" {
		item["xbot_path"] = path
		item["status"] = "completed"
	}
	if fail != "" {
		item["xbot_error"] = fail
	}
	return json.Marshal(item)
}

func (s *imageStore) save(id, result string) (string, string) {
	if id != "" {
		if p, ok := s.saved[id]; ok {
			return p, s.fails[id]
		}
		if f, ok := s.fails[id]; ok {
			return "", f
		}
	}
	path, fail := s.write(result)
	if id != "" {
		if path != "" {
			s.saved[id] = path
		}
		if fail != "" {
			s.fails[id] = fail
		}
	}
	return path, fail
}

func (s *imageStore) write(result string) (string, string) {
	if strings.TrimSpace(result) == "" {
		return "", "厂商未返回图片"
	}
	if !s.ok {
		return "", "没有可写的工作区"
	}
	if len(s.saved) >= maxImagesPerResponse {
		return "", fmt.Sprintf("单次回复最多保存 %d 张", maxImagesPerResponse)
	}
	if i := strings.Index(result, ";base64,"); strings.HasPrefix(result, "data:") && i >= 0 {
		result = result[i+len(";base64,"):]
	}
	// Reject before allocating decoded bytes; the workspace enforces the same
	// 20 MiB decoded limit before writing.
	if len(result) > base64.StdEncoding.EncodedLen(work.GeneratedMaxBytes) {
		return "", "图片超过 20 MiB"
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(result))
	if err != nil {
		return "", "图片数据无法解码"
	}
	path, err := s.gen.Save(data)
	if err != nil {
		return "", "保存失败: " + err.Error()
	}
	return path, ""
}

// imageGenerationPart summarizes a stripped image_generation_call. Its text is
// what the model sees on replay: the workspace path, or an honest failure.
func imageGenerationPart(raw json.RawMessage, p ContentPart) (ContentPart, error) {
	var it struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		RevisedPrompt string `json:"revised_prompt"`
		Prompt        string `json:"prompt"`
		Result        string `json:"result"`
		Path          string `json:"xbot_path"`
		Error         string `json:"xbot_error"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		return p, err
	}
	q := it.RevisedPrompt
	if q == "" {
		q = it.Prompt
	}
	p.Type = PartHosted
	// The stripped item is not a valid native replay (its result is gone), so
	// it replays as text.
	p.VendorRaw = nil
	p.Hosted = &HostedStep{ID: it.ID, Name: "image_generation", Status: it.Status, Query: q}
	switch {
	case it.Path != "":
		p.Text = "[厂商生图: 已保存到工作区 " + it.Path + "]"
	case it.Error != "":
		p.Hosted.Status = "failed"
		p.Text = "[厂商生图失败: " + it.Error + "]"
	case it.Result != "":
		// Only reachable when a caller parses an unstripped item.
		p.Hosted.Status = "failed"
		p.Text = "[厂商生图失败: 未保存]"
	case it.Status == "" || it.Status == "in_progress" || it.Status == "generating":
		p.Text = "[厂商生图: " + it.Status + "]"
	default:
		p.Hosted.Status = "failed"
		p.Text = "[厂商生图失败: 厂商未返回图片 · " + it.Status + "]"
	}
	return p, nil
}

// imageDeliveryGuidance also applies to prose produced in the generating
// response itself, before the model can see the saved-path receipt on replay.
const imageDeliveryGuidance = "厂商生图由 XBot 保存到工作区 generated/，保存回执只代表文件已落盘。交付图片时，先读取回执给出的真实路径，再调用 notify_owner，将该路径放入 images 挂进本轮回复气泡；若当前目录已改变，按工作区根目录解析该路径。仅在 notify_owner 挂图成功回执后说明「已用挂图方法挂上」；失败则如实说明原因。当前响应尚未见到保存回执时，继续用现有工具检查 generated/ 后读取下一步回执，不编造路径或图片 ID，不声称已经挂图。"

// HasImageGen reports whether ctx mounts hosted image generation.
func HasImageGen(ctx context.Context) bool {
	_, ok := imageGenFrom(ctx)
	return ok
}
