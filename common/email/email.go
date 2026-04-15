package email

import (
	"GoAI/config"
	"fmt"

	"gopkg.in/gomail.v2"
)

const (
	CodeMsg     = "GoAgent验证码如下(仅限于2分钟有效):"
	UserNameMsg = "GoAgent的账号如下"
)

func SendCaptcha(email, code, msg string) error {
	m := gomail.NewMessage()

	// 发件人
	m.SetHeader("From", config.GetConfig().Email)
	// 收件人
	m.SetHeader("To", email)
	// 主题
	m.SetHeader("Subject", "来自GoAgent平台的消息")
	// 正文(纯文本格式)
	m.SetBody("text/plain", msg+""+code)

	// 配置SMTP服务器和授权码,587(SMTP的明文/STARTTLS端口号)
	d := gomail.NewDialer("smtp.qq.com", 587, config.GetConfig().EmailConfig.Email, config.GetConfig().EmailConfig.Authcode)

	// 发送邮件
	if err := d.DialAndSend(m); err != nil {
		fmt.Printf("DialAndSend Error: %v\n", err)
		return err
	}

	fmt.Printf("send email success!\n")
	return nil
}
