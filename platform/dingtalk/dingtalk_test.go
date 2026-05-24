package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
)

// ──────────────────────────────────────────────────────────────
// Thread safety tests for token caching
// ──────────────────────────────────────────────────────────────

func TestGetAccessToken_ConcurrentAccess(t *testing.T) {
	// This test verifies that concurrent calls to getAccessToken
	// with a pre-cached token are properly synchronized by the mutex

	p := &Platform{
		clientID:     "test_client",
		clientSecret: "test_secret",
		httpClient:   &http.Client{}, // Valid HTTP client
		accessToken:  "test_token",   // Pre-cache a token
		tokenExpiry:  time.Now().Add(1 * time.Hour),
	}

	// Launch multiple goroutines to stress-test the mutex
	const numGoroutines = 100
	var wg sync.WaitGroup
	successCount := 0
	var countMu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := p.getAccessToken()
			if err == nil && token == "test_token" {
				countMu.Lock()
				successCount++
				countMu.Unlock()
			}
		}()
	}

	wg.Wait()

	// All goroutines should have gotten the cached token
	if successCount != numGoroutines {
		t.Errorf("expected %d successful token retrievals, got %d", numGoroutines, successCount)
	}

	t.Logf("Completed %d concurrent token requests without deadlock", numGoroutines)
}

func TestGetAccessToken_MutexExists(t *testing.T) {
	// Verify that the tokenMu mutex field exists and works
	p := &Platform{
		clientID:     "test_client",
		clientSecret: "test_secret",
	}

	// Test that we can lock/unlock the mutex (verify no panic under lock)
	p.tokenMu.Lock()
	_ = p.clientID // SA2001: intentional empty section to verify Lock/Unlock work
	p.tokenMu.Unlock()

	// Test with defer
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	t.Log("tokenMu mutex is functional")
}

func TestGetAccessToken_CachedTokenAccess(t *testing.T) {
	// Test that cached token access is thread-safe
	p := &Platform{
		clientID:     "test_client",
		clientSecret: "test_secret",
		accessToken:  "cached_token",
		tokenExpiry:  time.Now().Add(1 * time.Hour),
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	tokens := make([]string, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			token, err := p.getAccessToken()
			if err == nil {
				tokens[idx] = token
			}
		}(i)
	}

	wg.Wait()

	// Verify all goroutines got the same cached token
	for i, token := range tokens {
		if token != "" && token != "cached_token" {
			t.Errorf("goroutine %d: expected cached token 'cached_token', got %q", i, token)
		}
	}

	t.Logf("All %d goroutines safely accessed cached token", numGoroutines)
}

func TestPlatform_MutexFieldExists(t *testing.T) {
	// Verify the Platform struct has the tokenMu field
	p := &Platform{}

	// Verify no panic under lock (test will fail to compile if tokenMu doesn't exist)
	p.tokenMu.Lock()
	_ = p.clientID // SA2001: intentional empty section to verify Lock/Unlock work
	p.tokenMu.Unlock()

	t.Log("Platform.tokenMu field exists")
}

func TestPlatform_AccessTokenFieldsExist(t *testing.T) {
	// Verify the Platform struct has the token caching fields
	p := &Platform{}

	// Set the fields
	p.accessToken = "test_token"
	p.tokenExpiry = time.Now().Add(1 * time.Hour)

	// Verify they're set
	if p.accessToken != "test_token" {
		t.Errorf("expected accessToken 'test_token', got %q", p.accessToken)
	}

	t.Log("Platform token caching fields exist and are accessible")
}

// ──────────────────────────────────────────────────────────────
// ReconstructReplyCtx tests
// ──────────────────────────────────────────────────────────────

func TestReconstructReplyCtx_GroupSharedSession(t *testing.T) {
	p := &Platform{}
	rctx, err := p.ReconstructReplyCtx("dingtalk:g:conv123")
	if err != nil {
		t.Fatalf("ReconstructReplyCtx() error = %v", err)
	}
	rc := rctx.(replyContext)
	if rc.conversationId != "conv123" {
		t.Errorf("conversationId = %q, want %q", rc.conversationId, "conv123")
	}
	if rc.senderStaffId != "" {
		t.Errorf("senderStaffId = %q, want empty", rc.senderStaffId)
	}
	if !rc.isGroup {
		t.Error("isGroup = false, want true for group session")
	}
	if !rc.proactive {
		t.Error("proactive = false, want true")
	}
}

func TestReconstructReplyCtx_GroupPerUserSession(t *testing.T) {
	p := &Platform{}
	rctx, err := p.ReconstructReplyCtx("dingtalk:g:conv123:user456")
	if err != nil {
		t.Fatalf("ReconstructReplyCtx() error = %v", err)
	}
	rc := rctx.(replyContext)
	if rc.conversationId != "conv123" {
		t.Errorf("conversationId = %q, want %q", rc.conversationId, "conv123")
	}
	if rc.senderStaffId != "user456" {
		t.Errorf("senderStaffId = %q, want %q", rc.senderStaffId, "user456")
	}
	if !rc.isGroup {
		t.Error("isGroup = false, want true for group session")
	}
}

func TestReconstructReplyCtx_DirectSession(t *testing.T) {
	p := &Platform{}
	rctx, err := p.ReconstructReplyCtx("dingtalk:d:conv789:user111")
	if err != nil {
		t.Fatalf("ReconstructReplyCtx() error = %v", err)
	}
	rc := rctx.(replyContext)
	if rc.conversationId != "conv789" {
		t.Errorf("conversationId = %q, want %q", rc.conversationId, "conv789")
	}
	if rc.senderStaffId != "user111" {
		t.Errorf("senderStaffId = %q, want %q", rc.senderStaffId, "user111")
	}
	if rc.isGroup {
		t.Error("isGroup = true, want false for direct session")
	}
	if !rc.proactive {
		t.Error("proactive = false, want true")
	}
}

func TestOnMessage_DirectSessionKeepsSenderStaffIDWhenChannelSharingEnabled(t *testing.T) {
	p := &Platform{shareSessionInChannel: true}
	var got *core.Message
	p.handler = func(_ core.Platform, msg *core.Message) {
		got = msg
	}

	p.onMessage(&chatbot.BotCallbackDataModel{
		MsgId:            "msg-direct-shared-session",
		Msgtype:          "text",
		ConversationType: "1",
		ConversationId:   "direct-conv-1",
		SenderStaffId:    "staff-1",
		SenderNick:       "Alice",
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello"},
	}, nil)

	if got == nil {
		t.Fatal("handler was not called")
	}
	if got.SessionKey != "dingtalk:d:direct-conv-1:staff-1" {
		t.Fatalf("SessionKey = %q, want direct key with senderStaffId", got.SessionKey)
	}
}

func TestReconstructReplyCtx_InvalidPrefix(t *testing.T) {
	p := &Platform{}
	_, err := p.ReconstructReplyCtx("telegram:g:conv123")
	if err == nil {
		t.Fatal("expected error for non-dingtalk prefix")
	}
}

func TestReconstructReplyCtx_InvalidConvType(t *testing.T) {
	p := &Platform{}
	_, err := p.ReconstructReplyCtx("dingtalk:x:conv123")
	if err == nil {
		t.Fatal("expected error for invalid conversation type")
	}
}

func TestReconstructReplyCtx_EmptyConversationId(t *testing.T) {
	p := &Platform{}
	_, err := p.ReconstructReplyCtx("dingtalk:g:")
	if err == nil {
		t.Fatal("expected error for empty conversationId")
	}
}

func TestReconstructReplyCtx_TooFewParts(t *testing.T) {
	p := &Platform{}
	_, err := p.ReconstructReplyCtx("dingtalk:")
	if err == nil {
		t.Fatal("expected error for too few parts")
	}
}

// ──────────────────────────────────────────────────────────────
// formatReplyContent tests
// ──────────────────────────────────────────────────────────────

func TestFormatReplyContent_WithQuotedText(t *testing.T) {
	p := &Platform{}
	repliedContent, _ := json.Marshal(repliedTextContent{Text: "original message"})
	richText := &richTextContent{
		Content:    "user reply",
		IsReplyMsg: true,
		RepliedMsg: &repliedMessage{
			MsgType: "text",
			Content: repliedContent,
		},
	}
	result := p.formatReplyContent(richText, "fallback")
	expected := "引用: \"original message\"\n\nuser reply"
	if result != expected {
		t.Errorf("formatReplyContent() = %q, want %q", result, expected)
	}
}

func TestFormatReplyContent_EmptyContent_UsesFallback(t *testing.T) {
	p := &Platform{}
	repliedContent, _ := json.Marshal(repliedTextContent{Text: "quoted"})
	richText := &richTextContent{
		Content:    "",
		IsReplyMsg: true,
		RepliedMsg: &repliedMessage{
			MsgType: "text",
			Content: repliedContent,
		},
	}
	result := p.formatReplyContent(richText, "fallback text")
	expected := "引用: \"quoted\"\n\nfallback text"
	if result != expected {
		t.Errorf("formatReplyContent() = %q, want %q", result, expected)
	}
}

func TestFormatReplyContent_NilRepliedMsg(t *testing.T) {
	p := &Platform{}
	richText := &richTextContent{
		Content:    "just a message",
		IsReplyMsg: true,
		RepliedMsg: nil,
	}
	result := p.formatReplyContent(richText, "fallback")
	if result != "just a message" {
		t.Errorf("formatReplyContent() = %q, want %q", result, "just a message")
	}
}

func TestFormatReplyContent_NonTextMsgType(t *testing.T) {
	p := &Platform{}
	richText := &richTextContent{
		Content:    "user reply",
		IsReplyMsg: true,
		RepliedMsg: &repliedMessage{
			MsgType: "image",
			Content: json.RawMessage(`{}`),
		},
	}
	result := p.formatReplyContent(richText, "fallback")
	if result != "user reply" {
		t.Errorf("formatReplyContent() = %q, want %q", result, "user reply")
	}
}

func TestFormatReplyContent_EmptyQuotedText(t *testing.T) {
	p := &Platform{}
	repliedContent, _ := json.Marshal(repliedTextContent{Text: ""})
	richText := &richTextContent{
		Content:    "user reply",
		IsReplyMsg: true,
		RepliedMsg: &repliedMessage{
			MsgType: "text",
			Content: repliedContent,
		},
	}
	result := p.formatReplyContent(richText, "fallback")
	if result != "user reply" {
		t.Errorf("formatReplyContent() = %q, want %q", result, "user reply")
	}
}

// ──────────────────────────────────────────────────────────────
// Proactive routing tests
// ──────────────────────────────────────────────────────────────

func TestProactiveRouting_GroupSessionUsesGroupAPI(t *testing.T) {
	// Verify that a group session key produces a replyContext with isGroup=true,
	// which sendProactiveMessage would route to groupMessages/send.
	p := &Platform{}
	rctx, err := p.ReconstructReplyCtx("dingtalk:g:conv123:user456")
	if err != nil {
		t.Fatalf("ReconstructReplyCtx() error = %v", err)
	}
	rc := rctx.(replyContext)
	if !rc.isGroup || rc.conversationId == "" {
		t.Errorf("group routing: isGroup=%v, conversationId=%q; want isGroup=true with non-empty conversationId", rc.isGroup, rc.conversationId)
	}
}

func TestProactiveRouting_DirectSessionUsesDirectAPI(t *testing.T) {
	// Verify that a direct session key produces a replyContext with isGroup=false,
	// which sendProactiveMessage would route to oToMessages/batchSend.
	p := &Platform{}
	rctx, err := p.ReconstructReplyCtx("dingtalk:d:conv789:user111")
	if err != nil {
		t.Fatalf("ReconstructReplyCtx() error = %v", err)
	}
	rc := rctx.(replyContext)
	if rc.isGroup {
		t.Error("direct routing: isGroup=true, want false for 1:1 session")
	}
	if rc.senderStaffId != "user111" {
		t.Errorf("direct routing: senderStaffId=%q, want %q", rc.senderStaffId, "user111")
	}
}

// ──────────────────────────────────────────────────────────────
// extractRichText tests (from main: richText message type support)
// ──────────────────────────────────────────────────────────────

func TestExtractRichText(t *testing.T) {
	tests := []struct {
		name    string
		content interface{}
		want    string
	}{
		{
			name:    "nil content",
			content: nil,
			want:    "",
		},
		{
			name:    "non-map content",
			content: "not a map",
			want:    "",
		},
		{
			name: "empty richText array",
			content: map[string]interface{}{
				"richText": []interface{}{},
			},
			want: "",
		},
		{
			name: "single text element",
			content: map[string]interface{}{
				"richText": []interface{}{
					map[string]interface{}{"text": "Hello World"},
				},
			},
			want: "Hello World",
		},
		{
			name: "multiple text elements",
			content: map[string]interface{}{
				"richText": []interface{}{
					map[string]interface{}{"text": "Hello "},
					map[string]interface{}{"text": "World"},
				},
			},
			want: "Hello World",
		},
		{
			name: "text with attrs (bold etc) — attrs ignored, text extracted",
			content: map[string]interface{}{
				"richText": []interface{}{
					map[string]interface{}{"text": "normal "},
					map[string]interface{}{"text": "bold", "attrs": map[string]interface{}{"bold": true}},
				},
			},
			want: "normal bold",
		},
		{
			name: "mixed text and picture elements — pictures skipped",
			content: map[string]interface{}{
				"richText": []interface{}{
					map[string]interface{}{"text": "See image: "},
					map[string]interface{}{"pictureDownloadCode": "abc123"},
					map[string]interface{}{"text": "done"},
				},
			},
			want: "See image: done",
		},
		{
			name: "missing richText key",
			content: map[string]interface{}{
				"other": "data",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRichText(tt.content)
			if got != tt.want {
				t.Errorf("extractRichText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────
// Token expiry fallback when server returns missing/invalid expireIn
// ──────────────────────────────────────────────────────────────

// fakeAccessTokenRT serves a single canned /oauth2/accessToken response
// regardless of the request URL — enough to exercise getAccessToken's
// caching arithmetic without hitting the real DingTalk API.
type fakeAccessTokenRT struct {
	body string
}

func (f *fakeAccessTokenRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestGetAccessToken_ZeroExpireIn_FallsBackToDefault(t *testing.T) {
	p := &Platform{
		clientID:     "test_client",
		clientSecret: "test_secret",
		httpClient: &http.Client{
			Transport: &fakeAccessTokenRT{body: `{"accessToken":"tok-zero","expireIn":0}`},
		},
	}

	before := time.Now()
	tok, err := p.getAccessToken()
	if err != nil {
		t.Fatalf("getAccessToken() error = %v", err)
	}
	if tok != "tok-zero" {
		t.Fatalf("token = %q, want %q", tok, "tok-zero")
	}

	// Without the fallback, tokenExpiry would land at "before" (now+0s), making
	// time.Now().Before(tokenExpiry) immediately false — every subsequent call
	// would re-fetch a token. Assert the cache window is meaningful (>= 1h).
	gotWindow := p.tokenExpiry.Sub(before)
	if gotWindow < time.Hour {
		t.Errorf("tokenExpiry window = %v from response, want >= 1h (zero-expireIn should fall back, not cache for 0s)", gotWindow)
	}
}

func TestGetAccessToken_NegativeExpireIn_FallsBackToDefault(t *testing.T) {
	p := &Platform{
		clientID:     "test_client",
		clientSecret: "test_secret",
		httpClient: &http.Client{
			Transport: &fakeAccessTokenRT{body: `{"accessToken":"tok-neg","expireIn":-1}`},
		},
	}

	before := time.Now()
	if _, err := p.getAccessToken(); err != nil {
		t.Fatalf("getAccessToken() error = %v", err)
	}
	if p.tokenExpiry.Sub(before) < time.Hour {
		t.Errorf("tokenExpiry window for expireIn=-1 = %v, want >= 1h", p.tokenExpiry.Sub(before))
	}
}

func TestGetAccessToken_NormalExpireIn_AppliesBuffer(t *testing.T) {
	p := &Platform{
		clientID:     "test_client",
		clientSecret: "test_secret",
		httpClient: &http.Client{
			Transport: &fakeAccessTokenRT{body: `{"accessToken":"tok-7200","expireIn":7200}`},
		},
	}

	before := time.Now()
	if _, err := p.getAccessToken(); err != nil {
		t.Fatalf("getAccessToken() error = %v", err)
	}
	// 7200 - 300 buffer = 6900s = 115min. Allow tolerance for elapsed time.
	gotWindow := p.tokenExpiry.Sub(before)
	if gotWindow < 100*time.Minute || gotWindow > 116*time.Minute {
		t.Errorf("tokenExpiry window for expireIn=7200 = %v, want ~6900s (100-116min)", gotWindow)
	}
}

type captureDingTalkFileSendRT struct {
	t                *testing.T
	uploadSeen       bool
	sendSeen         bool
	sentRequestBody  map[string]any
	uploadContentTyp string
	wantSendPath     string
}

func (f *captureDingTalkFileSendRT) RoundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case req.URL.Host == "api.dingtalk.com" && req.URL.Path == "/v1.0/oauth2/accessToken":
		return jsonResponse(http.StatusOK, `{"accessToken":"tok-file","expireIn":7200}`), nil
	case req.URL.Host == "oapi.dingtalk.com" && req.URL.Path == "/media/upload":
		f.uploadSeen = true
		f.uploadContentTyp = req.Header.Get("Content-Type")
		if got := req.URL.Query().Get("access_token"); got != "tok-file" {
			f.t.Fatalf("upload access_token = %q, want tok-file", got)
		}
		if got := req.URL.Query().Get("type"); got != "file" {
			f.t.Fatalf("upload type = %q, want file", got)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			f.t.Fatalf("read upload body: %v", err)
		}
		if !bytes.Contains(body, []byte("biology pdf")) {
			f.t.Fatalf("upload body does not contain file bytes: %q", string(body))
		}
		if !bytes.Contains(body, []byte("lesson.pdf")) {
			f.t.Fatalf("upload body does not contain filename: %q", string(body))
		}
		return jsonResponse(http.StatusOK, `{"errcode":0,"errmsg":"ok","media_id":"media-file-1","type":"file"}`), nil
	case req.URL.Host == "api.dingtalk.com" && req.URL.Path == f.wantResolvedSendPath():
		f.sendSeen = true
		if got := req.Header.Get("x-acs-dingtalk-access-token"); got != "tok-file" {
			f.t.Fatalf("send token header = %q, want tok-file", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&f.sentRequestBody); err != nil {
			f.t.Fatalf("decode send body: %v", err)
		}
		return jsonResponse(http.StatusOK, `{}`), nil
	default:
		f.t.Fatalf("unexpected request: %s %s?%s", req.Method, req.URL.Path, req.URL.RawQuery)
		return nil, nil
	}
}

func (f *captureDingTalkFileSendRT) wantResolvedSendPath() string {
	if f.wantSendPath != "" {
		return f.wantSendPath
	}
	return "/v1.0/robot/oToMessages/batchSend"
}

func TestSendFile_UploadsMediaAndSendsFileMessage(t *testing.T) {
	rt := &captureDingTalkFileSendRT{t: t}
	p := &Platform{
		clientID:     "client-id",
		clientSecret: "client-secret",
		robotCode:    "robot-code",
		httpClient:   &http.Client{Transport: rt},
	}

	err := p.SendFile(contextBackgroundForTest(), replyContext{senderStaffId: "staff-1"}, core.FileAttachment{
		FileName: "lesson.pdf",
		Data:     []byte("biology pdf"),
	})
	if err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}
	if !rt.uploadSeen {
		t.Fatal("expected media upload request")
	}
	if !strings.HasPrefix(rt.uploadContentTyp, "multipart/form-data;") {
		t.Fatalf("upload content type = %q, want multipart/form-data", rt.uploadContentTyp)
	}
	if !rt.sendSeen {
		t.Fatal("expected file message send request")
	}
	if got := rt.sentRequestBody["robotCode"]; got != "robot-code" {
		t.Fatalf("robotCode = %#v, want robot-code", got)
	}
	users, ok := rt.sentRequestBody["userIds"].([]any)
	if !ok || len(users) != 1 || users[0] != "staff-1" {
		t.Fatalf("userIds = %#v, want [staff-1]", rt.sentRequestBody["userIds"])
	}
	if got := rt.sentRequestBody["msgKey"]; got != "sampleFile" {
		t.Fatalf("msgKey = %#v, want sampleFile", got)
	}
	msgParamRaw, ok := rt.sentRequestBody["msgParam"].(string)
	if !ok {
		t.Fatalf("msgParam = %#v, want JSON string", rt.sentRequestBody["msgParam"])
	}
	var msgParam map[string]string
	if err := json.Unmarshal([]byte(msgParamRaw), &msgParam); err != nil {
		t.Fatalf("decode msgParam %q: %v", msgParamRaw, err)
	}
	wantParam := map[string]string{
		"mediaId":  "media-file-1",
		"fileName": "lesson.pdf",
		"fileType": "pdf",
	}
	for key, want := range wantParam {
		if got := msgParam[key]; got != want {
			t.Fatalf("msgParam[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestSendFile_GroupSessionUsesGroupMessageAPI(t *testing.T) {
	rt := &captureDingTalkFileSendRT{
		t:            t,
		wantSendPath: "/v1.0/robot/groupMessages/send",
	}
	p := &Platform{
		clientID:     "client-id",
		clientSecret: "client-secret",
		robotCode:    "robot-code",
		httpClient:   &http.Client{Transport: rt},
	}

	err := p.SendFile(context.Background(), replyContext{
		conversationId: "group-conv-1",
		isGroup:        true,
		proactive:      true,
	}, core.FileAttachment{
		FileName: "lesson.pdf",
		Data:     []byte("biology pdf"),
	})
	if err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}
	if !rt.sendSeen {
		t.Fatal("expected group file message send request")
	}
	if got := rt.sentRequestBody["robotCode"]; got != "robot-code" {
		t.Fatalf("robotCode = %#v, want robot-code", got)
	}
	if got := rt.sentRequestBody["openConversationId"]; got != "group-conv-1" {
		t.Fatalf("openConversationId = %#v, want group-conv-1", got)
	}
	if _, ok := rt.sentRequestBody["userIds"]; ok {
		t.Fatalf("group file send should not include userIds: %#v", rt.sentRequestBody["userIds"])
	}
	if got := rt.sentRequestBody["msgKey"]; got != "sampleFile" {
		t.Fatalf("msgKey = %#v, want sampleFile", got)
	}
	msgParamRaw, ok := rt.sentRequestBody["msgParam"].(string)
	if !ok {
		t.Fatalf("msgParam = %#v, want JSON string", rt.sentRequestBody["msgParam"])
	}
	var msgParam map[string]string
	if err := json.Unmarshal([]byte(msgParamRaw), &msgParam); err != nil {
		t.Fatalf("decode msgParam %q: %v", msgParamRaw, err)
	}
	if got := msgParam["mediaId"]; got != "media-file-1" {
		t.Fatalf("msgParam mediaId = %q, want media-file-1", got)
	}
	if got := msgParam["fileName"]; got != "lesson.pdf" {
		t.Fatalf("msgParam fileName = %q, want lesson.pdf", got)
	}
	if got := msgParam["fileType"]; got != "pdf" {
		t.Fatalf("msgParam fileType = %q, want pdf", got)
	}
}

type captureDingTalkConvFileRT struct {
	t             *testing.T
	uploadInfo    map[string]any
	ossBody       []byte
	commitBody    map[string]any
	sendBody      map[string]any
	ossPutSeen    bool
	commitSeen    bool
	sendSeen      bool
	tokenRequests int
}

func (f *captureDingTalkConvFileRT) RoundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case req.URL.Host == "api.dingtalk.com" && req.URL.Path == "/v1.0/oauth2/accessToken":
		f.tokenRequests++
		return jsonResponse(http.StatusOK, `{"accessToken":"tok-file","expireIn":7200}`), nil
	case req.URL.Host == "api.dingtalk.com" && req.URL.Path == "/v2.0/storage/spaces/files/parent-uuid/uploadInfos/query":
		if got := req.URL.Query().Get("unionId"); got != "union-1" {
			f.t.Fatalf("uploadInfos unionId = %q, want union-1", got)
		}
		if got := req.Header.Get("x-acs-dingtalk-access-token"); got != "tok-file" {
			f.t.Fatalf("uploadInfos token = %q, want tok-file", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&f.uploadInfo); err != nil {
			f.t.Fatalf("decode uploadInfo body: %v", err)
		}
		return jsonResponse(http.StatusOK, `{
			"uploadKey":"upload-key-1",
			"headerSignatureInfo":{
				"resourceUrls":["https://oss.example.test/upload/path"],
				"headers":{"x-oss-token":"signed"}
			}
		}`), nil
	case req.URL.Host == "oss.example.test" && req.URL.Path == "/upload/path":
		f.ossPutSeen = true
		if req.Method != http.MethodPut {
			f.t.Fatalf("OSS method = %s, want PUT", req.Method)
		}
		if got := req.Header.Get("x-oss-token"); got != "signed" {
			f.t.Fatalf("OSS signed header = %q, want signed", got)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			f.t.Fatalf("read OSS body: %v", err)
		}
		f.ossBody = body
		return jsonResponse(http.StatusOK, ``), nil
	case req.URL.Host == "api.dingtalk.com" && req.URL.Path == "/v2.0/storage/spaces/files/parent-uuid/commit":
		f.commitSeen = true
		if got := req.URL.Query().Get("unionId"); got != "union-1" {
			f.t.Fatalf("commit unionId = %q, want union-1", got)
		}
		if got := req.Header.Get("x-acs-dingtalk-access-token"); got != "tok-file" {
			f.t.Fatalf("commit token = %q, want tok-file", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&f.commitBody); err != nil {
			f.t.Fatalf("decode commit body: %v", err)
		}
		return jsonResponse(http.StatusOK, `{"dentry":{"id":"dentry-1","spaceId":"space-1"}}`), nil
	case req.URL.Host == "api.dingtalk.com" && req.URL.Path == "/v1.0/convFile/conversations/files/send":
		f.sendSeen = true
		if got := req.URL.Query().Get("unionId"); got != "union-1" {
			f.t.Fatalf("convFile send unionId = %q, want union-1", got)
		}
		if got := req.Header.Get("x-acs-dingtalk-access-token"); got != "tok-file" {
			f.t.Fatalf("convFile send token = %q, want tok-file", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&f.sendBody); err != nil {
			f.t.Fatalf("decode convFile send body: %v", err)
		}
		return jsonResponse(http.StatusOK, `{}`), nil
	default:
		f.t.Fatalf("unexpected request: %s %s://%s%s?%s", req.Method, req.URL.Scheme, req.URL.Host, req.URL.Path, req.URL.RawQuery)
		return nil, nil
	}
}

func TestSendFile_ConvFileModeUploadsAndSendsGroupFile(t *testing.T) {
	rt := &captureDingTalkConvFileRT{t: t}
	p := &Platform{
		clientID:                 "client-id",
		clientSecret:             "client-secret",
		robotCode:                "robot-code",
		fileSendMode:             "conv_file",
		fileSendOperatorUnionID:  "union-1",
		fileSendParentDentryUUID: "parent-uuid",
		httpClient:               &http.Client{Transport: rt},
	}

	err := p.SendFile(context.Background(), replyContext{
		conversationId: "group-conv-1",
		isGroup:        true,
	}, core.FileAttachment{
		FileName: "lesson.pdf",
		Data:     []byte("biology pdf"),
	})
	if err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}

	if protocol := rt.uploadInfo["protocol"]; protocol != "HEADER_SIGNATURE" {
		t.Fatalf("upload protocol = %#v, want HEADER_SIGNATURE", protocol)
	}
	option, ok := rt.uploadInfo["option"].(map[string]any)
	if !ok {
		t.Fatalf("upload option = %#v, want object", rt.uploadInfo["option"])
	}
	preCheck, ok := option["preCheckParam"].(map[string]any)
	if !ok {
		t.Fatalf("upload preCheckParam = %#v, want object", option["preCheckParam"])
	}
	if got := preCheck["name"]; got != "lesson.pdf" {
		t.Fatalf("preCheck name = %#v, want lesson.pdf", got)
	}
	if got := preCheck["size"]; got != float64(len("biology pdf")) {
		t.Fatalf("preCheck size = %#v, want %d", got, len("biology pdf"))
	}
	if !rt.ossPutSeen {
		t.Fatal("expected OSS PUT upload")
	}
	if string(rt.ossBody) != "biology pdf" {
		t.Fatalf("OSS body = %q, want biology pdf", string(rt.ossBody))
	}
	if !rt.commitSeen {
		t.Fatal("expected storage commit request")
	}
	if got := rt.commitBody["uploadKey"]; got != "upload-key-1" {
		t.Fatalf("commit uploadKey = %#v, want upload-key-1", got)
	}
	if got := rt.commitBody["name"]; got != "lesson.pdf" {
		t.Fatalf("commit name = %#v, want lesson.pdf", got)
	}
	if !rt.sendSeen {
		t.Fatal("expected convFile send request")
	}
	wantSend := map[string]string{
		"spaceId":            "space-1",
		"dentryId":           "dentry-1",
		"openConversationId": "group-conv-1",
	}
	for key, want := range wantSend {
		if got := rt.sendBody[key]; got != want {
			t.Fatalf("convFile send %s = %#v, want %q", key, got, want)
		}
	}
	if rt.tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1 cached token request", rt.tokenRequests)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func contextBackgroundForTest() context.Context {
	return context.Background()
}
