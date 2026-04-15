package file

import (
	"GoAI/common/rag"
	"GoAI/config"
	"GoAI/utils"
	"context"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
)

// UploadRagFile 上传rag相关文件(仅文本)
func UploadRagFile(username string, file *multipart.FileHeader) (string, error) {
	/*
		流程：
			检验文件 -> 创建用户目录(uploads/username) -> 清除redis旧文件rag索引 -> 清除与该次rag无关的旧文件(避免本地空间膨胀也避免信息泄露风险)
	*/
	// 检验文件类型和文本名
	if err := utils.ValidateFile(file); err != nil {
		log.Printf("File validation failed: %v", err)
		return "", err
	}

	// 创建用户目录
	userDir := filepath.Join("uploads", username)
	if err := os.MkdirAll(userDir, 0755); err != nil { //权限 0755 表示：所有者可读、写、执行（7），同组用户和其他用户可读、执行（5）。
		log.Printf("Failed to create user dir %s: %v", userDir, err)
		return "", err
	}

	// 清理用户旧文件的关联索引
	files, err := os.ReadDir(userDir)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() {
				filename := f.Name()
				// 删除该文件对应的Redis索引
				if err := rag.DeleteIndex(context.Background(), filename); err != nil {
					log.Printf("Failed to delete index for %s: %v", filename, err)
					// 继续执行，不因为索引删除失败而中断文件上传
				}
			}
		}
	}

	// 删除用户目录下的所有旧的物理文件
	if err := utils.RemoveAllFilesInDir(userDir); err != nil {
		log.Printf("Failed to clean user dir %s: %v", userDir, err)
		return "", err
	}

	/*
		给上传文件进行前缀修改替换成新命名文件 -> 打开上传文件并将文件内容复制到新命名文件中 (完成持久化存储) -> RAG向量化数据
	*/

	// 生成UUID作为唯一文件名
	uuid := utils.GenerateUUID()
	ext := filepath.Ext(file.Filename) // 从上传文件的原始名称中提取扩展名（包含点号），比如 "简历.pdf" → ".pdf"，
	filename := uuid + ext             // 将 UUID 与扩展名拼接，得到类似 550e8400-e29b-41d4-a716-446655440000.pdf 的唯一文件名。
	// 将前面创建的用户目录（uploads/alice）与生成的文件名组合，得到最终在服务器上写入文件的完整路径，
	// 例如： uploads/alice/550e8400-e29b-41d4-a716-446655440000.pdf。
	filePath := filepath.Join(userDir, filename)

	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open uploaded file: %v", err)
		return "", err
	}
	defer src.Close()

	// 创建目标文件
	dst, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create destination file %s: %v", filePath, err)
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		log.Printf("Failed to copy file content: %v", err)
		return "", err
	}
	log.Printf("File uploaded successfully: %s", filePath)

	// 创建RAG索引器并对文件向量化
	// 根据配置的 Embedding 模型（如 text-embedding-ada-002）创建一个索引器实例，并绑定到当前 filename。
	// 将 filename 作为文档唯一标识存入向量数据库（如 Redis），方便后续检索溯源。
	indexer, err := rag.NewRAGIndexer(filename, config.GetConfig().RagModelConfig.RagEmbeddingModel)
	if err != nil {
		log.Printf("Failed to create RAG indexer: %v", err)
		// 删除已上传的文件
		os.Remove(filePath)
		return "", err
	}

	// 读取文件内容并创建向量索引
	// 读取 filePath 指向的物理文件内容（如 PDF、TXT）。
	// 对内容进行分段、清洗，调用 Embedding 模型生成向量。
	// 将向量与元数据存入向量存储（Redis、Milvus 等）。
	if err := indexer.IndexFile(context.Background(), filePath); err != nil {
		log.Printf("Failed to index file: %v", err)
		// 删除已上传的文件及索引
		os.Remove(filePath)
		rag.DeleteIndex(context.Background(), filename)
		return "", err
	}

	log.Printf("File indexed successfully: %s", filename)
	return filePath, nil
}
