package redis

import (
	"GoAI/config"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	redisCli "github.com/redis/go-redis/v9"
)

var Rdb *redisCli.Client

var ctx = context.Background()

func Init() {
	host := config.GetConfig().RedisConfig.RedisHost
	port := config.GetConfig().RedisConfig.RedisPort
	password := config.GetConfig().RedisConfig.RedisPassword
	db := config.GetConfig().RedisDb
	addr := host + ":" + strconv.Itoa(port)

	Rdb = redisCli.NewClient(&redisCli.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		Protocol: 2, // 使用 Protocol 2 避免 maint_notifications 警告
	})
}

// SetCaptchaForEmail 设置邮箱验证码
func SetCaptchaForEmail(email, captcha string) error {
	// 生成特定邮箱key
	key := GenerateCaptcha(email)
	expire := time.Minute * 2
	return Rdb.Set(ctx, key, captcha, expire).Err()
}

// CheckCaptchaForEmail 验证邮箱验证码
func CheckCaptchaForEmail(email, userInput string) (bool, error) {
	// 获取特定邮箱key
	key := GenerateCaptcha(email)

	// 在redis中查找邮箱验证码是否存在
	storedCaptcha, err := Rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redisCli.Nil {
			return false, nil
		}
		return false, err
	}

	if strings.EqualFold(storedCaptcha, userInput) {
		// 验证成功删除key
		if err := Rdb.Del(ctx, key).Err(); err != nil {
			fmt.Println("Delete EmailCaptcha Error:" + err.Error())
		}
		return true, nil
	}
	return false, nil
}

// InitRedisIndex 初始化 Redis 索引，支持按文件名区分
func InitRedisIndex(ctx context.Context, filename string, dimension int) error {
	indexName := GenerateIndexName(filename) // Key 格式

	// 检查索引是否存在
	_, err := Rdb.Do(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		fmt.Println("索引已存在，跳过创建")
		return nil
	}

	// 如果索引不存在，创建新索引
	if !strings.Contains(err.Error(), "Unknown index name") {
		return fmt.Errorf("检查索引失败: %w", err)
	}

	fmt.Println("正在创建 Redis 索引...")

	prefix := GenerateIndexNamePrefix(filename)

	// 创建索引
	createArgs := []interface{}{
		// 创建新索引, name; indexName
		"FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"SCHEMA",
		// 定义字段类型。content 和 metadata 都是文本字段
		"content", "TEXT",
		"metadata", "TEXT",
		// 告诉 Redis 这是一个向量字段，存储向量数据。
		"vector", "VECTOR", "FLAT",
		"6",
		// 向量里每个数字的类型
		"TYPE", "FLOAT32",
		// 向量维度
		"DIM", dimension,
		// 告诉 Redis，查相似向量的时候，用“余弦相似度”去比较两个向量是不是意思相近
		"DISTANCE_METRIC", "COSINE",
	}

	if err := Rdb.Do(ctx, createArgs...).Err(); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}

	fmt.Println("索引创建成功！")
	return nil
}

// DeleteRedisIndex 删除 Redis 索引，支持按文件名区分
func DeleteRedisIndex(ctx context.Context, filename string) error {
	indexName := GenerateIndexName(filename)

	// 删除索引
	if err := Rdb.Do(ctx, "FT.DROPINDEX", indexName).Err(); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}

	fmt.Println("索引删除成功！")
	return nil
}
