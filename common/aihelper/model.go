package aihelper

import (
	"GoAI/common/rag"
	"GoAI/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// AI 消息模型

type StreamCallback func(msg string)

// AIModel 定义AI模型接口
type AIModel interface {
	GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error)         // 同步生成回复。
	StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) // 流式生成回复，通过回调函数实时输出。
	GetModelType() string                                                                              // 返回模型类型。
}

// OpenAI
type OpenAIModel struct {
	llm model.ToolCallingChatModel
}

func NewOpenAIModel(ctx context.Context) (*OpenAIModel, error) {
	// key := os.Getenv("OPENAI_API_KEY")
	// modelName := os.Getenv("OPENAI_MODEL_NAME")
	// baseURL := os.Getenv("OPENAI_BASE_URL")

	key := config.GetConfig().ApiKey
	baseURL := "https://api.deepseek.com"
	modelName := "deepseek-chat"

	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create openai model failed: %v", err)
	}
	return &OpenAIModel{llm: llm}, nil
}

func (o *OpenAIModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("openai generate failed: %v", err)
	}
	return resp, nil
}

func (o *OpenAIModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("openai stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("openai stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content) // 聚合

			cb(msg.Content) // 实时调用cb函数，方便主动发送给前端
		}
	}

	return fullResp.String(), nil //返回完整内容，方便后续存储
}

func (o *OpenAIModel) GetModelType() string { return "openai" }

// =================== Ollama 实现 ===================

// OllamaModel Ollama模型实现
type OllamaModel struct {
	llm model.ToolCallingChatModel
}

func NewOllamaModel(ctx context.Context, baseURL, modelName string) (*OllamaModel, error) {
	llm, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
	})
	if err != nil {
		return nil, fmt.Errorf("create ollama model failed: %v", err)
	}
	return &OllamaModel{llm: llm}, nil
}

func (o *OllamaModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("ollama generate failed: %v", err)
	}
	return resp, nil
}

func (o *OllamaModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("ollama stream failed: %v", err)
	}
	defer stream.Close()
	var fullResp strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("openai stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content) // 聚合
			cb(msg.Content)                   // 实时调用cb函数，方便主动发送给前端
		}
	}
	return fullResp.String(), nil //返回完整内容，方便后续存储
}

func (o *OllamaModel) GetModelType() string { return "ollama" }

// =================== RAG 实现 ===================
type AliRAGModel struct {
	llm      model.ToolCallingChatModel
	username string // 用于获取用户的文档
}

func NewAliRAGModel(ctx context.Context, username string) (*AliRAGModel, error) {
	// key := os.Getenv("OPENAI_API_KEY")
	key := config.GetConfig().RagKey
	conf := config.GetConfig()
	modelName := conf.RagModelConfig.RagChatModelName
	baseURL := conf.RagModelConfig.RagBaseUrl

	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create ali rag model failed: %v", err)
	}
	return &AliRAGModel{
		llm:      llm,
		username: username,
	}, nil
}

func (o *AliRAGModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	// 1. 创建 RAG 查询器
	ragQuery, err := rag.NewRAGQuery(ctx, o.username)
	if err != nil {
		log.Printf("Failed to create RAG query (user may not have uploaded file): %v", err)
		// 如果用户没有上传文件，直接使用原始问题
		resp, err := o.llm.Generate(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("ali rag generate failed: %v", err)
		}
		return resp, nil
	}

	// 2. 获取用户最后一条消息作为查询
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	// 3. 检索相关文档
	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		log.Printf("Failed to retrieve documents: %v", err)
		// 检索失败，使用原始问题
		resp, err := o.llm.Generate(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("ali rag generate failed: %v", err)
		}
		return resp, nil
	}

	// 4. 构建包含检索结果的提示词
	ragPrompt := rag.BuildRAGPrompt(query, docs)

	// 5. 替换最后一条消息为 RAG 提示词
	ragMessages := make([]*schema.Message, len(messages))
	copy(ragMessages, messages)
	ragMessages[len(ragMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: ragPrompt,
	}

	// 6. 调用 LLM 生成回答
	resp, err := o.llm.Generate(ctx, ragMessages)
	if err != nil {
		return nil, fmt.Errorf("ali rag generate failed: %v", err)
	}
	return resp, nil
}

func (o *AliRAGModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	// 1. 创建 RAG 查询器
	ragQuery, err := rag.NewRAGQuery(ctx, o.username)
	if err != nil {
		log.Printf("Failed to create RAG query (user may not have uploaded file): %v", err)
		// 如果用户没有上传文件，直接使用原始问题
		return o.streamWithoutRAG(ctx, messages, cb)
	}

	// 2. 获取用户最后一条消息作为查询
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	// 3. 检索相关文档
	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		log.Printf("Failed to retrieve documents: %v", err)
		// 检索失败，使用原始问题
		return o.streamWithoutRAG(ctx, messages, cb)
	}

	// 4. 构建包含检索结果的提示词
	ragPrompt := rag.BuildRAGPrompt(query, docs)

	// 5. 替换最后一条消息为 RAG 提示词
	ragMessages := make([]*schema.Message, len(messages))
	copy(ragMessages, messages)
	ragMessages[len(ragMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: ragPrompt,
	}

	// 6. 流式调用 LLM
	stream, err := o.llm.Stream(ctx, ragMessages)
	if err != nil {
		return "", fmt.Errorf("ali rag stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("ali rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return fullResp.String(), nil
}

// streamWithoutRAG 当没有 RAG 文档时的流式响应
func (o *AliRAGModel) streamWithoutRAG(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("ali rag stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("ali rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return fullResp.String(), nil
}

func (o *AliRAGModel) GetModelType() string { return "2" }

// =================== MCP 实现 ===================

// MCPModel MCP模型实现，集成MCP服务
type MCPModel struct {
	llm        model.ToolCallingChatModel
	mcpClient  *client.Client
	username   string
	mcpBaseURL string
}

// NewMCPModel 创建MCP模型实例
func NewMCPModel(ctx context.Context, username string) (*MCPModel, error) {
	// key := os.Getenv("OPENAI_API_KEY")
	key := config.GetConfig().RagKey
	conf := config.GetConfig()
	modelName := conf.RagModelConfig.RagChatModelName
	baseURL := conf.RagModelConfig.RagBaseUrl

	// 创建LLM
	llm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  key,
	})
	if err != nil {
		return nil, fmt.Errorf("create mcp model failed: %v", err)
	}

	mcpBaseURL := "http://localhost:8081/mcp"

	return &MCPModel{
		llm:        llm,
		mcpBaseURL: mcpBaseURL,
		username:   username,
	}, nil
}

// getMCPClient 获取或创建MCP客户端
func (m *MCPModel) getMCPClient(ctx context.Context) (*client.Client, error) {
	if m.mcpClient == nil {
		// 创建MCP客户端
		httpTransport, err := transport.NewStreamableHTTP(m.mcpBaseURL)
		if err != nil {
			return nil, fmt.Errorf("create mcp transport failed: %v", err)
		}

		m.mcpClient = client.NewClient(httpTransport)

		// 初始化MCP客户端
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "MCP-Go AIHelper Client",
			Version: "1.0.0",
		}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}

		if _, err := m.mcpClient.Initialize(ctx, initRequest); err != nil {
			return nil, fmt.Errorf("mcp client initialize failed: %v", err)
		}
	}
	return m.mcpClient, nil
}

// GenerateResponse 生成响应，集成MCP工具
func (m *MCPModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	// 获取最后一条消息
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	// 第一次调用AI：告诉AI使用固定的JSON格式
	firstPrompt := m.buildFirstPrompt(query)
	firstMessages := make([]*schema.Message, len(messages))
	copy(firstMessages, messages)
	firstMessages[len(firstMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: firstPrompt,
	}

	// 调用LLM生成第一次响应
	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		return nil, fmt.Errorf("mcp first generate failed: %v", err)
	}
	log.Println("first resp is ", firstResp)
	// 解析AI响应
	aiResult := firstResp.Content
	toolCall, err := m.parseAIResponse(aiResult)
	if err != nil {
		log.Printf("Failed to parse AI response: %v", err)
		return firstResp, nil
	}

	// 情况1：AI不调用工具，直接返回响应
	if !toolCall.IsToolCall {
		log.Println("toolCall IsToolCall is false ", firstResp)
		return firstResp, nil
	}
	log.Println("toolCall IsToolCall is true ", firstResp)
	// 情况2：AI要调用工具
	// 获取MCP客户端
	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		log.Printf("MCP client error: %v", err)
		return firstResp, nil
	}

	// 调用MCP工具
	toolResult, err := m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
	if err != nil {
		log.Printf("MCP tool call failed: %v", err)
		return firstResp, nil
	}

	// 第二次调用AI：将工具结果告诉AI
	secondPrompt := m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult)
	secondMessages := make([]*schema.Message, len(messages))
	copy(secondMessages, messages)
	secondMessages[len(secondMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: secondPrompt,
	}

	// 调用LLM生成最终响应
	finalResp, err := m.llm.Generate(ctx, secondMessages)

	if err != nil {
		return nil, fmt.Errorf("mcp second generate failed: %v", err)
	}
	log.Println("最终响应为：", finalResp)
	return finalResp, nil
}

// StreamResponse 流式响应，集成MCP工具
func (m *MCPModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	// 获取最后一条消息
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	// 第一次调用AI：告诉AI使用固定的JSON格式
	firstPrompt := m.buildFirstPrompt(query)
	firstMessages := make([]*schema.Message, len(messages))
	copy(firstMessages, messages)
	firstMessages[len(firstMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: firstPrompt,
	}

	// 第一次调用使用同步接口（非流式）
	firstResp, err := m.llm.Generate(ctx, firstMessages)
	if err != nil {
		return "", fmt.Errorf("mcp first generate failed: %v", err)
	}

	aiResult := firstResp.Content
	toolCall, err := m.parseAIResponse(aiResult)
	if err != nil {
		log.Printf("Failed to parse AI response: %v", err)
		return aiResult, nil
	}

	// 情况1：AI不调用工具，直接返回响应
	if !toolCall.IsToolCall {
		return aiResult, nil
	}

	// 情况2：AI要调用工具
	// 获取MCP客户端
	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		log.Printf("MCP client error: %v", err)
		return aiResult, nil
	}

	// 调用MCP工具
	toolResult, err := m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
	if err != nil {
		log.Printf("MCP tool call failed: %v", err)
		return aiResult, nil
	}

	// 第二次调用AI：将工具结果告诉AI，使用流式接口
	secondPrompt := m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult)
	secondMessages := make([]*schema.Message, len(messages))
	copy(secondMessages, messages)
	secondMessages[len(secondMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: secondPrompt,
	}

	// 调用LLM生成最终响应（流式）
	stream, err := m.llm.Stream(ctx, secondMessages)
	if err != nil {
		return "", fmt.Errorf("mcp second stream failed: %v", err)
	}
	defer stream.Close()

	var finalResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("mcp second stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			finalResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return finalResp.String(), nil
}

// AIToolCall 表示AI工具调用请求
type AIToolCall struct {
	IsToolCall bool                   `json:"isToolCall"`
	ToolName   string                 `json:"toolName"`
	Args       map[string]interface{} `json:"args"`
}

// buildFirstPrompt 构建第一次调用的提示词
func (m *MCPModel) buildFirstPrompt(query string) string {
	prompt := fmt.Sprintf(`你是一个智能助手，可以调用MCP工具来获取实时信息。

==== 可用工具清单 ====

1) get_weather —— 获取指定城市的天气信息
   参数:
     - city (string, 必填): 城市名称，支持中英文，如 "北京"、"Shanghai"

2) query_ip —— 查询 IP 地址归属地信息
   参数:
     - ip (string, 必填): IP 地址，如 "8.8.8.8"，留空字符串则查询当前出口 IP

3) exchange_rate —— 查询货币汇率并换算金额
   参数:
     - from (string, 必填): 源货币代码，如 "USD"、"CNY"、"JPY"
     - to   (string, 必填): 目标货币代码，如 "CNY"、"EUR"
     - amount (number, 可选): 换算金额，默认 1

4) get_time —— 查询指定时区的当前时间
   参数:
     - timezone (string, 可选): IANA 时区名称，如 "Asia/Shanghai"、"America/New_York"；留空默认 "Asia/Shanghai"

==== 严格的输出规则 ====

1. 如果需要调用工具，必须只返回一个 JSON，不要包含任何其他文字、Markdown 标记或解释：
{
  "isToolCall": true,
  "toolName": "工具名称",
  "args": { "参数名": "参数值" }
}

2. 如果不需要调用工具，直接返回自然语言回答，不要包装成 JSON。

3. 只能选择上述工具之一，不得编造其他工具。

4. 参数值必须符合对应工具的参数类型和含义。

==== 示例 ====

用户: 北京今天天气怎么样？
输出: {"isToolCall": true, "toolName": "get_weather", "args": {"city": "北京"}}

用户: 8.8.8.8 是哪里的？
输出: {"isToolCall": true, "toolName": "query_ip", "args": {"ip": "8.8.8.8"}}

用户: 100 美元等于多少人民币？
输出: {"isToolCall": true, "toolName": "exchange_rate", "args": {"from": "USD", "to": "CNY", "amount": 100}}

用户: 纽约现在几点？
输出: {"isToolCall": true, "toolName": "get_time", "args": {"timezone": "America/New_York"}}

用户: 你好
输出: 你好！有什么可以帮您的吗？

==== 用户问题 ====
%s

请严格按照上述规则输出。`, query)

	log.Println(prompt)
	return prompt
}

// buildSecondPrompt 构建第二次调用的提示词
func (m *MCPModel) buildSecondPrompt(query, toolName string, args map[string]interface{}, toolResult string) string {
	return fmt.Sprintf(`你是一个智能助手，刚刚调用了MCP工具获取到真实数据，请基于工具返回结果组织自然、友好的最终回答。

==== 工具执行信息 ====
工具名称: %s
工具参数: %v
工具结果:
%s

==== 用户原始问题 ====
%s

==== 回答要求 ====
1. 用自然语言、简洁清晰地回答用户的问题。
2. 结合工具返回的真实数据作答，不要编造未提供的信息。
3. 如果工具结果包含数值（温度、汇率、时间等），请保留关键数字。
4. 不要再输出 JSON，也不要提及"工具"、"MCP"等内部实现细节。
5. 使用简体中文回答。`, toolName, args, toolResult, query)
}

// parseAIResponse 解析AI响应，检查是否包含工具调用
func (m *MCPModel) parseAIResponse(response string) (*AIToolCall, error) {
	// 支持 AI 输出带有 ```json 包裹的情况
	cleaned := cleanJSONWrapper(response)

	// 尝试直接解析为 JSON
	var toolCall AIToolCall
	if err := json.Unmarshal([]byte(cleaned), &toolCall); err == nil && toolCall.IsToolCall {
		if m.isValidToolName(toolCall.ToolName) {
			return &toolCall, nil
		}
	}

	// // 如果不是JSON，检查是否包含工具调用关键词
	// if strings.Contains(response, "get_weather") {
	// 	// 尝试提取城市名称
	// 	city := m.extractCityFromResponse(response)
	// 	if city != "" {
	// 		return &AIToolCall{
	// 			IsToolCall: true,
	// 			ToolName:   "get_weather",
	// 			Args:       map[string]interface{}{"city": city},
	// 		}, nil
	// 	}
	// }

	// 尝试从文本中抽取第一个 JSON 对象
	if jsonStr := extractFirstJSONObject(cleaned); jsonStr != "" {
		var tc AIToolCall
		if err := json.Unmarshal([]byte(jsonStr), &tc); err == nil && tc.IsToolCall {
			if m.isValidToolName(tc.ToolName) {
				return &tc, nil
			}
		}
	}

	// 兜底：根据关键词推断工具（尽量少用）
	if tc := m.fallbackGuessTool(response); tc != nil {
		return tc, nil
	}

	// 不是工具调用
	return &AIToolCall{IsToolCall: false}, nil
}

// isValidToolName 校验工具名是否在白名单内
func (m *MCPModel) isValidToolName(name string) bool {
	switch name {
	case "get_weather", "query_ip", "exchange_rate", "get_time":
		return true
	}
	return false
}

// callMCPTool 调用MCP工具
func (m *MCPModel) callMCPTool(ctx context.Context, client *client.Client, toolName string, args map[string]interface{}) (string, error) {
	if args == nil {
		args = map[string]interface{}{}
	}

	// 针对各工具做一次参数规范化 / 类型纠正
	args = m.normalizeToolArgs(toolName, args)

	callToolRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	result, err := client.CallTool(ctx, callToolRequest)
	if err != nil {
		return "", fmt.Errorf("mcp tool call failed: %v", err)
	}

	// 提取工具结果文本
	var text string
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			text += textContent.Text + "\n"
		}
	}

	log.Printf("MCP tool [%s] args=%v result=%s", toolName, args, text)
	return text, nil
}

// extractCityFromResponse 从响应中提取城市名称
// 直接从AI返回的JSON中提取城市，不预留城市列表
func (m *MCPModel) extractCityFromResponse(response string) string {
	// 尝试从JSON中提取城市
	var toolCall AIToolCall
	if err := json.Unmarshal([]byte(response), &toolCall); err == nil {
		if args, ok := toolCall.Args["city"].(string); ok {
			return args
		}
	}

	// 如果JSON解析失败，尝试从文本中提取城市名称
	// 这部分可以根据实际需要扩展，但不再预留固定城市列表
	return ""
}

// fallbackGuessTool 根据文本关键词做兜底猜测
func (m *MCPModel) fallbackGuessTool(response string) *AIToolCall {
	low := strings.ToLower(response)

	switch {
	case strings.Contains(low, "get_weather"):
		city := m.extractStringArg(response, "city")
		if city != "" {
			return &AIToolCall{
				IsToolCall: true,
				ToolName:   "get_weather",
				Args:       map[string]interface{}{"city": city},
			}
		}
	case strings.Contains(low, "query_ip"):
		ip := m.extractStringArg(response, "ip")
		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "query_ip",
			Args:       map[string]interface{}{"ip": ip},
		}
	case strings.Contains(low, "exchange_rate"):
		from := m.extractStringArg(response, "from")
		to := m.extractStringArg(response, "to")
		if from != "" && to != "" {
			args := map[string]interface{}{"from": from, "to": to}
			if amt := m.extractNumberArg(response, "amount"); amt != 0 {
				args["amount"] = amt
			}
			return &AIToolCall{
				IsToolCall: true,
				ToolName:   "exchange_rate",
				Args:       args,
			}
		}
	case strings.Contains(low, "get_time"):
		tz := m.extractStringArg(response, "timezone")
		return &AIToolCall{
			IsToolCall: true,
			ToolName:   "get_time",
			Args:       map[string]interface{}{"timezone": tz},
		}
	}
	return nil
}

// cleanJSONWrapper 去掉 Markdown ```json ... ``` 这类包裹
func cleanJSONWrapper(s string) string {
	s = strings.TrimSpace(s)
	// 去除 ```json 或 ``` 前后缀
	if strings.HasPrefix(s, "```") {
		// 去掉第一行 ```xxx
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		// 去掉末尾的 ```
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

// normalizeToolArgs 将 AI 返回的参数规范化为 MCP 工具期望的类型
func (m *MCPModel) normalizeToolArgs(toolName string, args map[string]interface{}) map[string]interface{} {
	switch toolName {
	case "exchange_rate":
		// amount 可能是字符串，需要转成 float64
		if v, ok := args["amount"]; ok {
			switch x := v.(type) {
			case string:
				if f, err := strconv.ParseFloat(x, 64); err == nil {
					args["amount"] = f
				} else {
					delete(args, "amount")
				}
			case int:
				args["amount"] = float64(x)
			case int64:
				args["amount"] = float64(x)
			}
		}
		// 货币代码统一大写
		if f, ok := args["from"].(string); ok {
			args["from"] = strings.ToUpper(strings.TrimSpace(f))
		}
		if t, ok := args["to"].(string); ok {
			args["to"] = strings.ToUpper(strings.TrimSpace(t))
		}

	case "get_time":
		if tz, ok := args["timezone"].(string); ok {
			args["timezone"] = strings.TrimSpace(tz)
		}

	case "query_ip":
		if ip, ok := args["ip"].(string); ok {
			args["ip"] = strings.TrimSpace(ip)
		}

	case "get_weather":
		if city, ok := args["city"].(string); ok {
			args["city"] = strings.TrimSpace(city)
		}
	}
	return args
}

// extractStringArg 从 JSON 风格文本里提取形如 "key": "value" 的字符串参数
func (m *MCPModel) extractStringArg(text, key string) string {
	// 简单的正则： "key"\s*:\s*"value"
	pattern := fmt.Sprintf(`"%s"\s*:\s*"([^"]*)"`, regexp.QuoteMeta(key))
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// extractNumberArg 提取形如 "key": 123.45 的数字参数
func (m *MCPModel) extractNumberArg(text, key string) float64 {
	pattern := fmt.Sprintf(`"%s"\s*:\s*([0-9]+(?:\.[0-9]+)?)`, regexp.QuoteMeta(key))
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 2 {
		if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return v
		}
	}
	return 0
}

// extractFirstJSONObject 抽取字符串中第一个完整的 JSON 对象
func extractFirstJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// GetModelType 获取模型类型
func (m *MCPModel) GetModelType() string { return "3" }

// Close 关闭MCP客户端
func (m *MCPModel) Close() {
	if m.mcpClient != nil {
		m.mcpClient.Close()
	}
}
