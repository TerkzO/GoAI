package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

/*
========================
公共 HTTP 工具函数
========================
*/

func httpGetJSON(ctx context.Context, apiURL string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("User-Agent", "mcp-multi-tools/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("json parse failed: %w", err)
		}
	}
	return nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: text},
		},
	}
}

/*
========================
	天气查询
========================
*/

// wttr.in JSON 响应结构
type WttrResponse struct {
	CurrentCondition []struct {
		TempC         string `json:"temp_C"`
		Humidity      string `json:"humidity"`
		WindspeedKmph string `json:"windspeedKmph"`
		WeatherDesc   []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
	} `json:"current_condition"`

	NearestArea []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
	} `json:"nearest_area"`
}

// 统一对外天气结构
type WeatherResponse struct {
	Location    string  `json:"location"`
	Temperature float64 `json:"temperature"`
	Condition   string  `json:"condition"`
	Humidity    int     `json:"humidity"`
	WindSpeed   float64 `json:"windSpeed"`
}

//Weather API Client

type WeatherAPIClient struct{}

func NewWeatherAPIClient() *WeatherAPIClient {
	return &WeatherAPIClient{}
}

// GetWeather 本质就是调用http请求获取天气信息
func (c *WeatherAPIClient) GetWeather(ctx context.Context, city string) (*WeatherResponse, error) {
	apiURL := fmt.Sprintf(
		"https://wttr.in/%s?format=j1&lang=zh",
		city,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	var wttrResp WttrResponse
	if err := json.Unmarshal(body, &wttrResp); err != nil {
		return nil, fmt.Errorf("json parse failed: %w", err)
	}

	if len(wttrResp.CurrentCondition) == 0 {
		return nil, fmt.Errorf("no weather data")
	}

	cc := wttrResp.CurrentCondition[0]

	temp, _ := strconv.ParseFloat(cc.TempC, 64)
	humidity, _ := strconv.Atoi(cc.Humidity)
	wind, _ := strconv.ParseFloat(cc.WindspeedKmph, 64)

	location := city
	if len(wttrResp.NearestArea) > 0 &&
		len(wttrResp.NearestArea[0].AreaName) > 0 {
		location = wttrResp.NearestArea[0].AreaName[0].Value
	}

	condition := "未知"
	if len(cc.WeatherDesc) > 0 {
		condition = cc.WeatherDesc[0].Value
	}

	return &WeatherResponse{
		Location:    location,
		Temperature: temp,
		Condition:   condition,
		Humidity:    humidity,
		WindSpeed:   wind,
	}, nil
}

/*
========================
 IP 地址归属地查询
========================
*/

type IPInfoResp struct {
	Status     string  `json:"status"`
	Country    string  `json:"country"`
	RegionName string  `json:"regionName"`
	City       string  `json:"city"`
	ISP        string  `json:"isp"`
	Query      string  `json:"query"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Timezone   string  `json:"timezone"`
	Message    string  `json:"message"`
}

func queryIP(ctx context.Context, ip string) (string, error) {
	apiURL := fmt.Sprintf("http://ip-api.com/json/%s?lang=zh-CN", url.PathEscape(ip))
	var r IPInfoResp
	if err := httpGetJSON(ctx, apiURL, &r); err != nil {
		return "", err
	}
	if r.Status != "success" {
		return "", fmt.Errorf("查询失败: %s", r.Message)
	}
	return fmt.Sprintf(
		"IP: %s\n国家: %s\n地区: %s\n城市: %s\nISP: %s\n经纬度: (%.4f, %.4f)\n时区: %s",
		r.Query, r.Country, r.RegionName, r.City, r.ISP, r.Lat, r.Lon, r.Timezone,
	), nil
}

/*
========================
 汇率查询（基于 exchangerate-api 开放接口）
========================
*/

type ExchangeResp struct {
	Result          string             `json:"result"`
	BaseCode        string             `json:"base_code"`
	ConversionRates map[string]float64 `json:"rates"`
}

func getExchangeRate(ctx context.Context, from, to string, amount float64) (string, error) {
	apiURL := fmt.Sprintf("https://open.er-api.com/v6/latest/%s", url.PathEscape(from))
	var r ExchangeResp
	if err := httpGetJSON(ctx, apiURL, &r); err != nil {
		return "", err
	}
	rate, ok := r.ConversionRates[to]
	if !ok {
		return "", fmt.Errorf("不支持的目标货币: %s", to)
	}
	converted := amount * rate
	return fmt.Sprintf(
		"%.2f %s = %.2f %s\n当前汇率: 1 %s = %.4f %s",
		amount, from, converted, to, from, rate, to,
	), nil
}

/*
========================
 当前时间查询（支持时区）
========================
*/

func getTimeNow(tz string) (string, error) {
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("无效的时区: %s", tz)
	}
	now := time.Now().In(loc)
	return fmt.Sprintf("时区: %s\n当前时间: %s\n星期: %s",
		tz, now.Format("2006-01-02 15:04:05"), now.Weekday().String()), nil
}

/*
========================
MCP Server
========================
*/
func NewMCPServer() *server.MCPServer {
	weatherClient := NewWeatherAPIClient()

	mcpServer := server.NewMCPServer(
		"multi-tools-server",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithLogging(),
	)

	// ---------- 天气 ----------
	mcpServer.AddTool(
		mcp.NewTool(
			"get_weather",
			mcp.WithDescription("获取指定城市的天气信息"),
			mcp.WithString(
				"city",
				mcp.Description("城市名称，如 Beijing、上海"),
				mcp.Required(),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			city, ok := args["city"].(string)
			if !ok || city == "" {
				return nil, fmt.Errorf("invalid city argument")
			}

			weather, err := weatherClient.GetWeather(ctx, city)
			if err != nil {
				return nil, err
			}

			resultText := fmt.Sprintf(
				"城市: %s\n温度: %.1f°C\n天气: %s\n湿度: %d%%\n风速: %.1f km/h",
				weather.Location,
				weather.Temperature,
				weather.Condition,
				weather.Humidity,
				weather.WindSpeed,
			)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: resultText,
					},
				},
			}, nil
		},
	)

	// ---------- IP 查询 ----------
	mcpServer.AddTool(
		mcp.NewTool("query_ip",
			mcp.WithDescription("查询 IP 地址归属地信息"),
			mcp.WithString("ip", mcp.Description("IP 地址，如 8.8.8.8，留空则查询本机出口 IP"), mcp.Required()),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			ip, _ := args["ip"].(string)
			text, err := queryIP(ctx, ip)
			if err != nil {
				return nil, err
			}
			return textResult(text), nil
		},
	)

	// ---------- 汇率 ----------
	mcpServer.AddTool(
		mcp.NewTool("exchange_rate",
			mcp.WithDescription("查询两种货币之间的汇率并换算金额"),
			mcp.WithString("from", mcp.Description("源货币代码，如 USD、CNY"), mcp.Required()),
			mcp.WithString("to", mcp.Description("目标货币代码，如 CNY、JPY"), mcp.Required()),
			mcp.WithNumber("amount", mcp.Description("换算金额，默认 1")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			from, _ := args["from"].(string)
			to, _ := args["to"].(string)
			amount, ok := args["amount"].(float64)
			if !ok {
				amount = 1
			}
			if from == "" || to == "" {
				return nil, fmt.Errorf("from/to 不能为空")
			}
			text, err := getExchangeRate(ctx, from, to, amount)
			if err != nil {
				return nil, err
			}
			return textResult(text), nil
		},
	)

	// ---------- 时间查询 ----------
	mcpServer.AddTool(
		mcp.NewTool("get_time",
			mcp.WithDescription("查询指定时区的当前时间"),
			mcp.WithString("timezone", mcp.Description("IANA 时区名称，如 Asia/Shanghai、America/New_York")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			tz, _ := args["timezone"].(string)
			text, err := getTimeNow(tz)
			if err != nil {
				return nil, err
			}
			return textResult(text), nil
		},
	)

	return mcpServer
}

// StartServer 启动MCP服务器
// httpAddr: HTTP服务器监听的地址（例如":8080"）
func StartServer(httpAddr string) error {
	mcpServer := NewMCPServer()

	httpServer := server.NewStreamableHTTPServer(mcpServer)
	log.Printf("HTTP MCP server listening on %s/mcp", httpAddr)
	return httpServer.Start(httpAddr)
}
