package account

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

// Mail is one outgoing message. It never carries a password.
type Mail struct {
	To      string
	Subject string
	Body    string
}

// Mailer sends account mail. The hosted service plugs a real sender in here.
type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// OutboxMailer is the stand-in sender: each message becomes a 0600 file in Dir,
// for the person running the service to open. Nothing leaves the machine.
type OutboxMailer struct{ Dir string }

func (o OutboxMailer) Send(_ context.Context, m Mail) error {
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return err
	}
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + ".txt"
	body := fmt.Sprintf("To: %s\nSubject: %s\n\n%s\n", m.To, m.Subject, strings.TrimSpace(m.Body))
	return fsutil.WriteAtomic(filepath.Join(o.Dir, name), []byte(body), 0o600)
}

func verifyMail(to, link, code string, login bool) Mail {
	subject := "确认你的 XBot 账号"
	lead := "点开下面的链接，完成注册："
	if login {
		subject = "登录 XBot"
		lead = "点开下面的链接即可登录："
	}
	body := lead + "\n\n" + link + "\n\n也可以在 XBot 里输入验证码：" + code + "\n\n链接和验证码 15 分钟内有效，只能用一次。不是你本人操作，可以忽略这封邮件。"
	return Mail{To: to, Subject: subject, Body: body}
}
