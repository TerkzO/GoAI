package model

import (
	"time"

	"gorm.io/gorm"
)

type Session struct {
	ID        string         `json:"id" gorm:"primaryKey;type:varchar(36)"`
	UserName  string         `json:"username" gorm:"index;not null"`
	Title     string         `json:"title" gorm:"type:varchar(100)"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"name"`
}
