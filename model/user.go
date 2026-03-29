package model

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        int64          `gorm:"primaryKey" json:"id"`                          // 主键 自增
	Name      string         `gorm:"type: varchar(50)" json:"name"`                 // 用户昵称
	Email     string         `gorm:"type: varchar(100)" json:"email"`               // 邮箱地址，支持索引，用于注册和验证码发送。
	Username  string         `gorm:"type: varchar(50);uniqueIndex" json:"username"` // 唯一用户名，用于登录
	Password  string         `gorm:"type: varchar(255)" json:"-"`                   // MD5加密,不返回给前端
	CreatedAt time.Time      `json:"create_at"`                                     // 自动时间戳
	UpdatedAt time.Time      `json:"update_at"`
	DeteledAt gorm.DeletedAt `gorm:"index" json:"-"` // 软删除
}
