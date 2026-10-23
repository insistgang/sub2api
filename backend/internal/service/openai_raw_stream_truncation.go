package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// openAIRawStreamTruncatedUpstreamMessage 是 raw CC 直转路径上游截断的 ops 消息。
const openAIRawStreamTruncatedUpstreamMessage = "Upstream Chat Completions stream ended before any terminal chunk"

// openAIRawStreamTerminalState 记录 raw Chat Completions SSE 流是否收到过
// **终止信号**。
//
// 背景：CC 直转路径把上游 SSE 原样透传，此前只要 HTTP 状态是 200 就按成功收尾——
// 上游中途断流（Cloudflare edge reset、后端 worker 掉线）会被伪装成
// `HTTP 200 + usage 0/0`：客户端拿到半截回答，网关既不报错也不计入 SLA，
// Ops 侧完全不可见。
//
// 三种终止信号任一出现即认为上游"讲完了"，只是尾巴可能丢失，不作截断处理：
//
//   - [DONE]        —— OpenAI CC 协议标准哨兵
//   - usage chunk   —— include_usage 生效时的末尾用量帧（网关强制打开）
//   - finish_reason —— 生成正常结束（stop/length/tool_calls/...）
//
// 只认 [DONE] 会误伤那些跑完最后一帧就直接 EOF 的兼容上游；只认 usage 会误伤
// 不支持 include_usage 的上游。三者取并集，把误判压到"上游确实在生成中途被切断"。
type openAIRawStreamTerminalState struct {
	// sawDataLine 表示上游至少发过一行 `data:`，即响应确实是 SSE 语义流。
	// 非 SSE 响应体（上游对 stream 请求返回裸 JSON）不参与截断判定，保持既有透传行为。
	sawDataLine     bool
	sawDone         bool
	sawUsage        bool
	sawFinishReason bool
}

// ObserveDataLine 从单行 SSE `data:` 载荷中提取终止信号。payload 需已 TrimSpace。
func (t *openAIRawStreamTerminalState) ObserveDataLine(payload string) {
	if t == nil {
		return
	}
	t.sawDataLine = true
	if payload == "[DONE]" {
		t.sawDone = true
		return
	}
	if usage := gjson.Get(payload, "usage"); usage.Exists() && usage.IsObject() {
		t.sawUsage = true
	}
	if t.sawFinishReason {
		return
	}
	for _, choice := range gjson.Get(payload, "choices").Array() {
		// finish_reason 为 null 时 String() 返回空串，不算终止。
		if strings.TrimSpace(choice.Get("finish_reason").String()) != "" {
			t.sawFinishReason = true
			return
		}
	}
}

// Terminated 表示上游给出过终止信号。
func (t *openAIRawStreamTerminalState) Terminated() bool {
	return t != nil && (t.sawDone || t.sawUsage || t.sawFinishReason)
}

// IsTruncated 判定上游是否在任何终止信号之前结束。clientOutputStarted 用于放行
// 非 SSE 响应体：那类响应本就没有 data: 行，既有行为是原样透传，不在本次判定范围内；
// 但"一个字节都没收到"的空 200 依然算截断。
func (t *openAIRawStreamTerminalState) IsTruncated(clientOutputStarted bool) bool {
	if t == nil || t.Terminated() {
		return false
	}
	return t.sawDataLine || !clientOutputStarted
}

// MiniMax may finish native Chat streams with successful base_resp metadata and
// EOF instead of [DONE]. Verify completion before normalizing that convention;
// the more permissive terminal policy above remains unchanged for other vendors.
type miniMaxRawChatCompletionState struct {
	expectedChoices    int64
	finishedChoices    map[int64]bool
	sawSuccessMetadata bool
	sawDone            bool
	endedWithSeparator bool
	invalid            bool
}

var errMiniMaxRawChatCompletionInvalid = errors.New("MiniMax Chat Completions stream has invalid completion metadata")

func newMiniMaxRawChatCompletionState(requestBody []byte) *miniMaxRawChatCompletionState {
	expected := int64(1)
	if n := gjson.GetBytes(requestBody, "n"); n.Exists() {
		expected = n.Int()
		if n.Type != gjson.Number || n.Float() != float64(expected) {
			expected = 0
		}
	}
	return &miniMaxRawChatCompletionState{expectedChoices: expected}
}

func (t *miniMaxRawChatCompletionState) ObserveLine(line string) {
	if t == nil {
		return
	}
	t.endedWithSeparator = line == ""
	if t.invalid || line == "" || strings.HasPrefix(line, ":") {
		return
	}
	if event, ok := extractOpenAISSEEventLine(line); ok {
		t.invalid = strings.EqualFold(event, "error")
		return
	}
	payload, ok := extractOpenAISSEDataLine(line)
	if !ok {
		t.invalid = !strings.HasPrefix(line, "id:") && !strings.HasPrefix(line, "retry:")
		return
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return
	}
	if payload == "[DONE]" {
		t.sawDone = true
		return
	}
	var chunk struct {
		Type    string          `json:"type"`
		Object  string          `json:"object"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Index        *int64                     `json:"index"`
			Delta        map[string]json.RawMessage `json:"delta"`
			FinishReason *string                    `json:"finish_reason"`
		} `json:"choices"`
		BaseResp *struct {
			StatusCode *int64 `json:"status_code"`
		} `json:"base_resp"`
	}
	if json.Unmarshal([]byte(payload), &chunk) != nil || chunk.Choices == nil ||
		chunk.Type == "error" || chunk.Object == "error" ||
		(len(chunk.Error) > 0 && strings.TrimSpace(string(chunk.Error)) != "null") || t.sawDone {
		t.invalid = true
		return
	}
	if gjson.Get(payload, "base_resp").Exists() {
		if chunk.BaseResp == nil || chunk.BaseResp.StatusCode == nil || *chunk.BaseResp.StatusCode != 0 {
			t.invalid = true
			return
		}
		t.sawSuccessMetadata = true
	}
	if t.finishedChoices == nil {
		t.finishedChoices = make(map[int64]bool)
	}
	seen := make(map[int64]bool, len(chunk.Choices))
	for _, choice := range chunk.Choices {
		if choice.Index == nil || *choice.Index < 0 || *choice.Index >= t.expectedChoices ||
			choice.Delta == nil || seen[*choice.Index] || t.finishedChoices[*choice.Index] {
			t.invalid = true
			return
		}
		seen[*choice.Index] = true
		finished := choice.FinishReason != nil
		if finished {
			switch *choice.FinishReason {
			case "stop", "length", "tool_calls", "content_filter", "function_call":
			default:
				t.invalid = true
				return
			}
		}
		t.finishedChoices[*choice.Index] = finished
	}
}

func (t *miniMaxRawChatCompletionState) Complete() bool {
	if t == nil || t.invalid || t.expectedChoices <= 0 ||
		int64(len(t.finishedChoices)) != t.expectedChoices || (!t.sawDone && !t.sawSuccessMetadata) {
		return false
	}
	for _, finished := range t.finishedChoices {
		if !finished {
			return false
		}
	}
	return true
}

// newOpenAIRawStreamTruncatedFailoverError 处理"上游截断且尚未向客户端写出任何
// 字节"的情况：响应头还没提交，可以透明换号重试，客户端不会看到半截流。
func newOpenAIRawStreamTruncatedFailoverError(
	c *gin.Context,
	account *Account,
	upstreamRequestID string,
	cause error,
) *UpstreamFailoverError {
	recordOpenAIRawStreamTruncation(c, account, upstreamRequestID, cause, "failover")

	headers := http.Header{}
	if id := strings.TrimSpace(upstreamRequestID); id != "" {
		headers.Set("x-request-id", id)
	}
	return &UpstreamFailoverError{
		StatusCode:      http.StatusBadGateway,
		ResponseBody:    openAIRawStreamTruncatedErrorBody(cause),
		ResponseHeaders: headers,
	}
}

// recordOpenAIRawStreamTruncation 把上游截断记入 ops 上下文，使其在错误日志与
// 账号健康度中可见——这正是此前"HTTP 200 假成功"丢掉的信息。
func recordOpenAIRawStreamTruncation(
	c *gin.Context,
	account *Account,
	upstreamRequestID string,
	cause error,
	kind string,
) {
	if c == nil {
		return
	}
	message := openAIRawStreamTruncatedMessage(cause)
	platform := PlatformOpenAI
	accountID := int64(0)
	accountName := ""
	if account != nil {
		platform = account.Platform
		accountID = account.ID
		accountName = account.Name
	}

	setOpsUpstreamError(c, http.StatusBadGateway, message, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           platform,
		AccountID:          accountID,
		AccountName:        accountName,
		UpstreamStatusCode: http.StatusBadGateway,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Kind:               kind,
		Message:            message,
	})
}

// openAIRawStreamTruncatedMessage 拼出 ops 消息：干净 EOF 没有底层错误可带，
// 传输层错误（connection reset / http2 stream error）则保留原因以便定位。
func openAIRawStreamTruncatedMessage(cause error) string {
	if cause == nil || errors.Is(cause, ErrOpenAIUpstreamStreamTruncated) {
		return openAIRawStreamTruncatedUpstreamMessage
	}
	return openAIRawStreamTruncatedUpstreamMessage + ": " + cause.Error()
}

// openAIRawStreamTruncatedErrorBody 构造 failover 错误体，code/message 与
// 写出后走 openAIUpstreamStreamReadError 的客户端分类保持一致。
func openAIRawStreamTruncatedErrorBody(cause error) []byte {
	code, message := classifyOpenAIUpstreamStreamReadError(cause)
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    "upstream_error",
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"upstream_error","code":"` + OpenAIUpstreamStreamTruncatedCode +
			`","message":"Upstream response stream ended before completion"}}`)
	}
	return body
}
