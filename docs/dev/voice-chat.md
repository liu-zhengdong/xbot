# 语音交流（一等公民音频）

日期：2026-10-30

状态：实现中（政东钉立刻做，不当候选搁置）

## 做完长什么样

1. 用户在聊天输入区按住（或点按）录音，松手后发出的是**语音气泡**（音频文件），不是自动转成文字进输入框。
2. 主列双方都能点播放语音泡；transcript 只记文件名，字节在 Bot 工作区。
3. Bot 收到的是语音文件；**怎么听**由它调工具（如听写/转写），内核不替它偷偷改成文字消息顶替语音。
4. Bot 发语音：自己用 TTS（或等价）做成音频文件，再经「发消息挂音频」挂到气泡（对齐生图：生成与挂泡分缝）。

不做：实时双向通话、电话、打断、按住即 STT 进输入框当默认路径。

## 装配缝（给编码）

对齐现有 `images` / `paste` / 发消息挂图：

- **媒体落盘 + transcript 字段**：用户上传与 Bot 生成的音频进工作区子目录（建议 `voice/`，与 `paste/` 并列）；行上 `audios: []`（或扩成通用 attachments，但本批可先 `audios` 平行 `images`）。
- **HTTP**：`POST .../voice`（或扩 paste 认音频 MIME）→ id；`POST .../messages` 接受 `audios`；`GET` 取回可播。
- **UI**：录音控件 + 语音泡播放器；切 Bot / 增量刷新路径要认 audios。
- **工具缝**：`listen`（读工作区音频→转写/摘要，经现有供应商 audio/STT）；`speak`/`tts`（文本→落盘路径）；**发消息工具只负责挂音频到泡**，不兼做 TTS。
- 供应商：优先走已登录/已配密钥的 Completions 兼容 audio 端点；没有可用后端时工具诚实失败，UI 不假装能录进模型。

## 对照回顾（摘）

| 项 | 关系 |
|---|---|
| 生图 / 挂图 | 同构：生成落盘 vs 发消息挂附件 |
| view_image | 听语音对称为 listen 工具 |
| 增量刷新 tip dbda0b60 | 新泡类型须走 commitTranscript key，勿逼整表重建 |

## 验收怎样算对（沿用 IA 定稿）

1. 录音发出去是语音泡，输入框没被填进转写字
2. 用户泡与 Bot 泡都能点播放
3. 无通话／电话 UI
4. 浅深各一态可读

**冒烟用例**：`e2e/smoke/voice_test.go` 的 `TestVoiceChatBinary`（真 serve，仅 chat/audio 后端 scripted）；`internal/web/voice_real_browser_test.go`（原生 MediaRecorder 与播放器，测试麦克风）；`internal/web/voice_browser_test.go`（取消、失败、草稿、切 Bot、增量刷新）。HTTP／工具边界由 `internal/web/voice_test.go`、`internal/kernel/audio_test.go`、`internal/work/voice_test.go` 覆盖。

## 还没做的

- 真实供应商音频服务与真实麦克风／扬声器验收；自动验证只用 fake/scripted。
- 用户日用确认；本批不移入 guard，不合 main、不 push。

接口协议依据：[OpenAI 转写接口](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create)、[语音合成](https://developers.openai.com/api/docs/guides/text-to-speech)。实际工具允许覆盖 provider/model/voice，不保证任意兼容聊天服务都支持音频端点。
