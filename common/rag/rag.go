package rag

import (
	"GoAI/common/redis"
	redisPkg "GoAI/common/redis"
	"GoAI/config"
	"context"
	"fmt"
	"log"
	"os"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

// 做小抄
// 封装完整的文档索引逻辑，包含 Embedder 和 Indexer
// 在 Index 阶段，系统会将原始文档内容进行解析和切分，把每一段文本转换成向量，并将这些向量存储到 Redis 的 vector 字段中。
type RAGIndexer struct {
	embedding embedding.Embedder // embedding是文本和向量之间转化的桥梁
	indexer   *redisIndexer.Indexer
}

// 翻小抄
// 封装完整的文档检索逻辑，包含 Embedder 和 Retriever
// Index 阶段负责把知识转换成 AI 能理解和搜索的形式并存储起来，
// 而 Query 阶段则负责从这些已经向量化的知识中检索出最相关的内容，再交给大模型生成最终答案。
type RAGQuery struct {
	embedding embedding.Embedder
	retriever retriever.Retriever
}

// 构建知识库索引
// 专业说法：文本解析、文本切块、向量化、存储向量
// 通俗理解：把“人能读的文档”，转换成“AI 能按语义搜索的格式”，并存起来
// 调用顺序:
// (file_service)UploadRagFile传递存储在本地的filename -> 调用NewRAGIndexer,
// 1. 先创建“向量生成器”(Embedding)
// 2. 初始化 Redis 中的向量索引结构
// 3. 配置索引器（定义：文档如何被存进 Redis）
// 4. 创建最终可用的索引器实例
// 5. 返回一个RAGIndexer
// NewRAGIndexer -> 回到(file_service)UploadRagFile 调用IndexFile传入
func NewRAGIndexer(filename, embeddingModel string) (*RAGIndexer, error) {

	// 用于控制整个初始化流程（超时 / 取消等），这里先用默认背景即可
	ctx := context.Background()

	// 从环境变量中读取调用向量模型所需的 API Key
	// apiKey := os.Getenv("OPENAI_API_KEY")
	apiKey := config.GetConfig().RagKey

	// 向量的维度大小（等于向量模型输出的数字个数）
	// Redis 在创建向量索引时必须提前知道这个值
	dimension := config.GetConfig().RagModelConfig.RagDimension

	// 1. 配置并创建“向量生成器”（Embedding）
	// 可以理解为：找一个“翻译官”，
	// 专门负责把文本翻译成 AI 能理解的“向量表示” (负责将文本翻译成向量（如 [0.12, -0.45, ...])
	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: config.GetConfig().RagModelConfig.RagBaseUrl, // 向量模型服务地址
		APIKey:  apiKey,                                       // 鉴权信息
		Model:   embeddingModel,                               // 使用哪个向量模型
	}
	// 创建向量生成器实例
	// 后续所有文本的“向量化”都会通过它完成
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// ===============================
	// 2. 初始化 Redis 中的向量索引结构
	// ===============================
	// 可以理解为：先在 Redis 里建好“仓库”，
	// 告诉它以后要存向量，并且每个向量的维度是多少
	if err := redisPkg.InitRedisIndex(ctx, filename, dimension); err != nil {
		return nil, fmt.Errorf("failed to init redis index: %w", err)
	}

	// 获取 Redis 客户端，用于后续数据写入
	rdb := redisPkg.Rdb

	// ===============================
	// 3. 配置索引器（定义：文档如何被存进 Redis）
	// ===============================
	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    rdb,                                     // Redis 客户端
		KeyPrefix: redis.GenerateIndexNamePrefix(filename), // 不同知识库使用不同前缀，避免冲突
		BatchSize: 10,                                      // 批量处理文档，提高写入效率

		// 定义：一段文档（Document）在 Redis 中该如何存储
		DocumentToHashes: func(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {

			// 从文档的元数据中取出来源信息（例如文件名、URL）
			source := ""
			if s, ok := doc.MetaData["source"].(string); ok {
				source = s
			}

			// 构造 Redis 中实际存储的数据结构（Hash）
			return &redisIndexer.Hashes{
				// Redis Key，一般由“知识库名 + 文档块 ID”组成
				Key: fmt.Sprintf("%s:%s", filename, doc.ID),

				// Redis Hash 中的字段
				Field2Value: map[string]redisIndexer.FieldValue{
					// content：原始文本内容
					// EmbedKey 表示：该字段需要先做向量化，
					// 生成的向量会存入名为 "vector" 的字段中
					"content": {Value: doc.Content, EmbedKey: "vector"},

					// metadata：一些辅助信息，不参与向量计算
					"metadata": {Value: source},
				},
			}, nil
		},
	}

	// 将“向量生成器”交给索引器
	// 这样索引器在写入文本时，可以自动完成向量计算
	indexerConfig.Embedding = embedder

	// ===============================
	// 4. 创建最终可用的索引器实例
	// ===============================
	// 此时索引器已经具备：
	// - 文本 → 向量 的能力
	// - 向量写入 Redis 的能力
	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	// 返回一个封装好的 RAGIndexer，
	// 后续只需要调用它，就可以把文档加入知识库
	return &RAGIndexer{
		embedding: embedder,
		indexer:   idx,
	}, nil
}

// IndexFile 读取文件内容并创建向量索引
func (r *RAGIndexer) IndexFile(ctx context.Context, filePath string) error {
	// 一次性读取文件全部内容
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// 将文件内容转换为文档
	// TODO: 这里可以根据需要进行文本切块，目前简单处理为一个文档
	doc := &schema.Document{
		ID:      "doc_1", // 可以使用 UUID 或其他唯一标识
		Content: string(content),
		MetaData: map[string]any{
			"source": filePath,
		},
	}

	// 使用 indexer 存储文档（会自动进行向量化）
	_, err = r.indexer.Store(ctx, []*schema.Document{doc})
	if err != nil {
		return fmt.Errorf("failed to store document: %w", err)
	}

	return nil
}

// DeleteIndex 删除指定文件的知识库索引（静态方法，不依赖实例）
func DeleteIndex(ctx context.Context, filename string) error {
	if err := redisPkg.DeleteRedisIndex(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete redis index: %w", err)
	}
	return nil
}

// NewRAGQuery 创建 RAG 查询器（用于向量检索和问答）
func NewRAGQuery(ctx context.Context, username string) (*RAGQuery, error) {
	log.Println("创建RAG查询器")
	cfg := config.GetConfig()
	// apiKey := os.Getenv("OPENAI_API_KEY")
	apiKey := config.GetConfig().RagKey

	// 配置EmbeddingConfig
	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: cfg.RagModelConfig.RagBaseUrl,
		APIKey:  apiKey,
		Model:   cfg.RagModelConfig.RagEmbeddingModel,
	}
	// 创建 embedding 模型
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// 获取用户上传的文件名（假设每个用户只有一个文件）
	// 这里需要从用户目录读取文件名
	userDir := fmt.Sprintf("uploads/%s", username)
	files, err := os.ReadDir(userDir)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no uploaded file found for user %s", username)
	}

	var filename string
	for _, f := range files {
		if !f.IsDir() {
			filename = f.Name()
			break
		}
	}

	log.Println("获取用户上传的文件名:", filename)

	if filename == "" {
		return nil, fmt.Errorf("no valid file found for user %s", username)
	}

	// 创建 retriever
	rdb := redisPkg.Rdb
	indexName := redis.GenerateIndexName(filename)

	retrieverConfig := &redisRetriever.RetrieverConfig{
		Client:       rdb,
		Index:        indexName,
		Dialect:      2,
		ReturnFields: []string{"content", "metadata", "distance"},
		TopK:         5,
		VectorField:  "vector",
		// 由于 Redis 存储的文档格式和 Eino 框架内部定义的 schema.Document 格式不同
		// 因此在查询时需要通过 DocumentConverter 进行相应的转换，
		// 将 Redis 返回的数据整理成框架内部可直接使用的 Document 对象。
		DocumentConverter: func(ctx context.Context, doc redisCli.Document) (*schema.Document, error) {
			resp := &schema.Document{
				ID:       doc.ID,
				Content:  "",
				MetaData: map[string]any{},
			}
			for field, val := range doc.Fields {
				if field == "content" {
					resp.Content = val
				} else {
					resp.MetaData[field] = val
				}
			}
			return resp, nil
		},
	}
	retrieverConfig.Embedding = embedder

	rtr, err := redisRetriever.NewRetriever(ctx, retrieverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create retriever: %w", err)
	}

	return &RAGQuery{
		embedding: embedder,
		retriever: rtr,
	}, nil
}

/*
1. 处理查询词 (Embedding)：
系统接收到你传入的 query（比如一个文本问题：“这篇文章讲了什么？”）。在底层，Retriever 通常会先调用大模型的 Embedding 接口，把这段文本也转换成一组向量（Vector）。

2. 指向特定的数据库和表 (使用配置)：
根据之前 RetrieverConfig 的配置：
    Retriever 会通过 Client: rdb 连接到 Redis。
    直接锁定 Index: indexName（也就是根据你之前 filename 生成的那个索引），确保只在当前用户上传的那个文件的数据里进行查找。

3. 执行向量相似度检索 (Vector Search)：
Retriever 会把你刚刚生成的 query 向量，发送到 Redis 数据库，并让 Redis 对比索引库里 VectorField: "vector" 字段中存储的所有向量。
    Redis 会计算它们之间的“距离”（通常是余弦相似度等）。
    根据你配置的 TopK: 5，Redis 会找出与你的 query 向量距离最近（即语义最相关）的 5 个数据块。

4. 组装并返回结果 (DocumentConverter)：
Redis 查出这 5 条数据后，会返回你配置的 ReturnFields: []string{"content", "metadata", "distance"}。
接着，底层会自动调用你之前写的 DocumentConverter 函数：
    把 Redis 返回的 content 填入 schema.Document 的 Content 中。
    把其他的字段塞进 MetaData 字典里。
    最后，把这 5 个组装好的文档作为 []schema.Document 返回给 docs 变量。
*/
// RetrieveDocuments 检索相关文档
func (r *RAGQuery) RetrieveDocuments(ctx context.Context, query string) ([]*schema.Document, error) {
	log.Println("检索相关文档...")
	docs, err := r.retriever.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve documents: %w", err)
	}
	return docs, nil
}

// BuildRAGPrompt 构建包含检索文档的提示词
func BuildRAGPrompt(query string, docs []*schema.Document) string {
	if len(docs) == 0 {
		return query
	}

	contextText := ""
	for i, doc := range docs {
		contextText += fmt.Sprintf("[文档 %d]: %s\n\n", i+1, doc.Content)
	}

	prompt := fmt.Sprintf(`基于以下参考文档回答用户的问题。如果文档中没有相关信息，请说明无法找到相关信息。

参考文档：
%s

用户问题：%s

请提供准确、完整的回答：`, contextText, query)

	return prompt
}
