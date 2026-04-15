package model

import "time"

type Message struct {
	ID        uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	SessionID string    `json:"session_id" gorm:"index;not null;type:varchar(36)"`
	UserName  string    `json:"username" gorm:"type:varchar(20)"`
	Content   string    `json:"content" gorm:"type:text"`
	IsUser    bool      `json:"is_user" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
}

type History struct {
	IsUser  bool   `json:"is_user"`
	Content string `json:"content"`
}
