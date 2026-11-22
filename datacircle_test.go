package datacircle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestDefinitions(t *testing.T) {
	tools := New(nil)
	if len(tools.Tools()) != 4 || len(tools.ResponseTools()) != 4 || len(tools.Functions()) != 4 {
		t.Fatalf("%d, %d and %d tools, want 4", len(tools.Tools()), len(tools.ResponseTools()), len(tools.Functions()))
	}
	// As the client sends them: the tools in a chat completion's request and in a response's.
	chat, err := json.Marshal(openai.ChatCompletionNewParams{Model: "m", Tools: tools.Tools()})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := json.Marshal(responses.ResponseNewParams{Model: "m", Tools: tools.ResponseTools()})
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Tools []struct {
			Type     string         `json:"type"`
			Function map[string]any `json:"function"`
		} `json:"tools"`
	}
	var sentResp struct {
		Tools []map[string]any `json:"tools"`
	}
	if json.Unmarshal(chat, &sent) != nil || json.Unmarshal(resp, &sentResp) != nil || len(sent.Tools) != 4 || len(sentResp.Tools) != 4 {
		t.Fatalf("sent %s and %s", chat, resp)
	}
	for i, name := range Names {
		f, r := sent.Tools[i].Function, sentResp.Tools[i]
		if sent.Tools[i].Type != "function" || f["name"] != name || f["description"] != abouts[name] {
			t.Errorf("chat tool %d: %+v", i, sent.Tools[i])
		}
		if r["type"] != "function" || r["name"] != name || r["description"] != abouts[name] || r["strict"] != false {
			t.Errorf("response tool %d: %+v", i, r)
		}
		for _, params := range []any{f["parameters"], r["parameters"]} {
			p, _ := json.Marshal(params)
			switch name {
			case BestPlansName, ProviderName:
				if !strings.Contains(string(p), `"required":["slug"]`) {
					t.Errorf("%s requires no slug: %s", name, p)
				}
			default:
				if strings.Contains(string(p), `"required"`) {
					t.Errorf("%s requires an input: %s", name, p)
				}
			}
			if name == SearchPlansName && !strings.Contains(string(p), `"enum":["score","provider","price"]`) {
				t.Errorf("%s's sort has no enum: %s", name, p)
			}
			if !strings.Contains(string(p), `"limit":{"description":"How many`) || !strings.Contains(string(p), `"type":"object"`) {
				t.Errorf("%s's parameters: %s", name, p)
			}
		}
	}
}

func TestCalls(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch {
		case r.URL.Path == "/apis":
			w.Write([]byte(`[{"api":"Employee profile","slug":"linkedin-profile","sheetPage":"s"},{"api":"Employee email","slug":"email-finder","plans":3}]`))
		case r.URL.Path == "/apis/email-finder/best":
			w.Write([]byte(`{"api":"Employee email","plans":[{"provider":"A","rank":1,"terms":"long"}]}`))
		case r.URL.Path == "/plans" && (r.URL.Query().Get("api") == "Bright Data" || r.URL.Query().Get("api") == "bright-data"):
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"api must be one of: Employee email"}`))
		case r.URL.Path == "/plans" && strings.Contains(r.URL.Query().Get("query"), "-"):
			w.Write([]byte(`{"total":0,"plans":[]}`))
		case r.URL.Path == "/plans":
			w.Write([]byte(`{"total":1,"plans":[{"provider":"Bright Data","rank":2}]}`))
		case r.URL.Path == "/providers/bright-data":
			w.Write([]byte(`{"provider":"Bright Data","apis":[{"api":"Employee profile","plans":[{"plan":"C"}]},{"api":"Employee email","rank":4,"plans":[{"plan":"B","rank":9},{"plan":"A","rank":4}]}]}`))
		case r.URL.Path == "/providers/down":
			w.WriteHeader(502)
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"no API has the slug nope"}`))
		}
	}))
	defer server.Close()
	tools := New(&Config{BaseURL: server.URL})
	ctx := context.Background()
	for _, call := range []struct {
		name, arguments string
		fails           bool
		has             string
	}{
		{ListAPIsName, `{"query":"email"}`, false, `"total":1`},
		{ListAPIsName, `{}`, false, `"total":2`},
		{BestPlansName, `{"slug":"Email Finder","limit":"2"}`, false, `"provider":"A"`},
		{SearchPlansName, `{"api":"Bright Data"}`, false, `No API is named`},
		{SearchPlansName, `{"api":"Bright Data","query":"growth"}`, true, `No API is named "Bright Data". datacircle_list_apis gives every API's name and slug.`},
		{SearchPlansName, `{"api":"bright-data","query":null}`, false, `these are the plans whose name holds \"bright data\".","plans":[{"provider":"Bright Data"`},
		{SearchPlansName, `{"query":"bright-data","sort":"price","limit":null}`, false, `"plans":[{"provider":"Bright Data"`},
		{ProviderName, `{"slug":"Bright Data"}`, false, `"apis":[{"api":"Employee email","best":{"plan":"A"`},
		{ProviderName, `{"slug":"bright-data","limit":1}`, false, `"ranked":1,"total":2`},
		{ProviderName, `{"slug":"bright-data","limit":1}`, false, `{"about":"Bright Data has plans on 2 APIs on Datacircle and a rank on 1 of them (1 is the best). Its best rank: 4, on Employee email.","apis":[`},
		{BestPlansName, `{"slug":"nope"}`, true, `no API has the slug nope. datacircle_list_apis gives every API's slug. A provider's plans: datacircle_search_plans`},
		{BestPlansName, `{"provider":"Bright Data","limit":2}`, true, `Give an API's slug, like email-finder: datacircle_list_apis gives every slug. A provider's plans:`},
		{ListAPIsName, `{"query":"Bright Data"}`, false, `"note":"No API's name holds every word of \"Bright Data\". A provider's plans: datacircle_search_plans`},
		{ProviderName, `{"slug":"down"}`, true, `datacircle: 502 Bad Gateway`},
		{BestPlansName, `{"slug":"email-finder","limit":[2]}`, true, `The arguments are no JSON object of this tool's parameters`},
		{ProviderName, `{"slug":"nope"}`, true, `no API has the slug nope. datacircle_search_plans with words of its name as query gives each plan's provider.`},
		{ListAPIsName, ``, false, `"total":2`},
		{"get_weather", `{}`, true, `datacircle: no tool is named "get_weather"`},
	} {
		got, err := tools.Run(ctx, call.name, call.arguments)
		if err != nil {
			got = err.Error()
		}
		if (err != nil) != call.fails || !strings.Contains(got, call.has) {
			t.Errorf("%s(%s) = %s, %v; want it to hold %s", call.name, call.arguments, got, err, call.has)
		}
		if strings.Contains(got, "terms") || strings.Contains(got, "sheetPage") {
			t.Errorf("%s gave a key it drops: %s", call.name, got)
		}
	}
	got, _ := tools.Run(ctx, ListAPIsName, `{"query":"employee email"}`)
	if strings.Contains(got, "note") {
		t.Errorf("datacircle_list_apis notes a query an API's name holds every word of: %s", got)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := tools.Run(canceled, ListAPIsName, `{}`); err != context.Canceled {
		t.Errorf("a canceled call gave %v", err)
	}
	if want := "/apis/email-finder/best?limit=2"; paths[2] != want {
		t.Errorf("called %s, want %s", paths[2], want)
	}
	if want := "/plans?query=bright+data&sort=price"; paths[10] != want {
		t.Errorf("called %s, want %s", paths[10], want)
	}

	// A chat completion's tool calls, decoded as the client decodes them, into their tool messages.
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(`{"id":"x","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[
		{"id":"call_1","type":"function","function":{"name":"datacircle_best_plans","arguments":"{\"slug\":\"email-finder\"}"}},
		{"id":"call_2","type":"function","function":{"name":"datacircle_best_plans","arguments":"{\"slug\":\"nope\"}"}},
		{"id":"call_3","type":"function","function":{"name":"get_weather","arguments":"{}"}},
		{"id":"call_4","type":"custom","custom":{"name":"datacircle_list_apis","input":"x"}}]}}]}`), &completion); err != nil {
		t.Fatal(err)
	}
	var answers []string
	for _, call := range completion.Choices[0].Message.ToolCalls {
		if msg, ok := tools.Call(ctx, call); ok {
			sent, _ := json.Marshal(msg)
			answers = append(answers, string(sent))
		}
	}
	if len(answers) != 2 || !strings.HasPrefix(answers[0], `{"content":"{\"api\":\"Employee email\",\"plans\":[{\"provider\":\"A\",\"rank\":1}]}","tool_call_id":"call_1","role":"tool"}`) ||
		answers[1] != `{"content":"{\"error\":\"no API has the slug nope. datacircle_list_apis gives every API's slug. A provider's plans: datacircle_search_plans with its name as query, like bright data, or datacircle_provider.\"}","tool_call_id":"call_2","role":"tool"}` {
		t.Errorf("Call gave %q", answers)
	}

	// A response's output, decoded as the client decodes it: its function calls of the four tools.
	var response responses.Response
	if err := json.Unmarshal([]byte(`{"id":"resp_1","output":[{"type":"reasoning","id":"r","summary":[]},
		{"type":"function_call","id":"fc1","call_id":"c1","name":"datacircle_list_apis","arguments":"{\"query\":\"email\"}"},
		{"type":"function_call","id":"fc2","call_id":"c2","name":"get_weather","arguments":"{}"},
		{"type":"message","id":"m","role":"assistant","content":[]},
		{"type":"function_call","id":"fc3","call_id":"c3","name":"datacircle_provider","arguments":"{\"slug\":\"down\"}"}]}`), &response); err != nil {
		t.Fatal(err)
	}
	outs := tools.Outputs(ctx, response.Output)
	sent, err := json.Marshal(responses.ResponseNewParams{Model: "m", PreviousResponseID: openai.String(response.ID),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: outs}})
	if err != nil || len(outs) != 2 || !strings.Contains(string(sent), `"input":[{"call_id":"c1","output":"{\"apis\":[{\"api\":\"Employee email\",\"plans\":3,\"slug\":\"email-finder\"}],\"total\":1}","type":"function_call_output"},{"call_id":"c3","output":"{\"error\":\"datacircle: 502 Bad Gateway\"}","type":"function_call_output"}]`) {
		t.Errorf("Outputs gave %s, %v", sent, err)
	}
}

// TestLive calls each tool once on the API itself: DATACIRCLE_LIVE=1 go test -run Live.
func TestLive(t *testing.T) {
	if os.Getenv("DATACIRCLE_LIVE") == "" {
		t.Skip("DATACIRCLE_LIVE is unset")
	}
	tools := New(nil)
	ctx := context.Background()
	for _, call := range []struct{ name, arguments, has string }{
		{ListAPIsName, `{"query":"email finder","limit":3}`, `"slug":"email-finder"`},
		{BestPlansName, `{"slug":"email-finder","limit":3}`, `"rank":1`},
		{SearchPlansName, `{"query":"bright data","sort":"price","limit":3}`, `"provider":"Bright Data"`},
		{ProviderName, `{"slug":"Bright Data","limit":3}`, `"about":"Bright Data has plans on`},
		{BestPlansName, `{"slug":"bright-data"}`, `"error":`},
	} {
		item, ok := tools.Output(ctx, responses.ResponseFunctionToolCall{CallID: "live", Name: call.name, Arguments: call.arguments})
		sent, _ := json.Marshal(item)
		if !ok || !strings.Contains(string(sent), strings.ReplaceAll(call.has, `"`, `\"`)) {
			t.Errorf("%s(%s) = %s; want it to hold %s", call.name, call.arguments, sent, call.has)
		}
		t.Logf("%s(%s) = %.300s", call.name, call.arguments, sent)
	}
}
