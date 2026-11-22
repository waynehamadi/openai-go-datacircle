# Datacircle tools for the OpenAI Go SDK

Find the best GTM APIs. We benchmark all the GTM APIs and we put the results on our website: https://datacircle.dev

This Go module gives the [OpenAI Go SDK](https://github.com/openai/openai-go) four function tools on Datacircle's open API, https://api.datacircle.dev/v1. You need no key and no account.

| Tool | What the tool gives | Call |
| --- | --- | --- |
| `datacircle_list_apis` | Every GTM API on Datacircle, with the API's slug, number of plans, number of providers and best plan | `GET /apis` |
| `datacircle_best_plans` | One API's plans, sorted by score out of 100, best first. Each plan comes with a rank and a price per 1,000 calls | `GET /apis/{slug}/best` |
| `datacircle_search_plans` | Plans that match a provider or plan name, on all APIs or on one API. Sort by score, provider or price | `GET /plans` |
| `datacircle_provider` | The APIs a provider sells, and the provider's rank on each API | `GET /providers/{slug}` |

## Install

```bash
go get github.com/waynehamadi/openai-go-datacircle
```

It works with `github.com/openai/openai-go/v3`, v3.0.0 and later.

## Chat completions

`Tools` gives the four tools for a request's `Tools`. `Call` runs a tool call of the model's message into the tool message that answers it.

```go
import (
	"github.com/openai/openai-go/v3"
	datacircle "github.com/waynehamadi/openai-go-datacircle"
)

client := openai.NewClient() // OPENAI_API_KEY
tools := datacircle.New(nil)
params := openai.ChatCompletionNewParams{
	Model:    "gpt-5.2",
	Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Find the 3 best providers for the email-finder API.")},
	Tools:    tools.Tools(),
}
for {
	completion, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		log.Fatal(err)
	}
	msg := completion.Choices[0].Message
	params.Messages = append(params.Messages, msg.ToParam())
	if len(msg.ToolCalls) == 0 {
		fmt.Println(msg.Content)
		break
	}
	for _, call := range msg.ToolCalls {
		answer, _ := tools.Call(ctx, call)
		params.Messages = append(params.Messages, answer)
	}
}
```

Your own tools can go in the same request: `append(tools.Tools(), yours...)`. `Call` gives `false` for a call of a tool that isn't one of the four, so you run it yourself. `Handles(name)` says the same before a call. A streamed reply works the same way: give each tool call of the `ChatCompletionAccumulator`'s message to `Call`.

## Responses API

`ResponseTools` gives the four tools for a response's `Tools`. `Outputs` runs the function calls of a response and gives a `function_call_output` item for each one.

```go
import "github.com/openai/openai-go/v3/responses"

params := responses.ResponseNewParams{
	Model: "gpt-5.2",
	Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("Find the 3 best providers for the email-finder API.")},
	Tools: tools.ResponseTools(),
}
for {
	response, err := client.Responses.New(ctx, params)
	if err != nil {
		log.Fatal(err)
	}
	outputs := tools.Outputs(ctx, response.Output)
	if len(outputs) == 0 {
		fmt.Println(response.OutputText())
		break
	}
	params.PreviousResponseID = openai.String(response.ID)
	params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: outputs}
}
```

`Outputs` gives nothing for a call of another function, so you run those yourself. `Output` runs one call, a `responses.ResponseFunctionToolCall`. For a server that keeps no responses, send the whole conversation instead: each call as `responses.ResponseInputItemParamOfFunctionCall(call.Arguments, call.CallID, call.Name)`, then the outputs. `Functions` gives the four function definitions, for `openai.ChatCompletionFunctionTool`.

## Call a tool yourself

```go
out, err := datacircle.New(nil).Run(ctx, datacircle.BestPlansName, `{"slug":"email-finder","limit":3}`)
```

Each tool gives back a JSON object. Each plan comes with its rank, provider, plan, score, price per 1,000 (`perThousand`, in dollars) and its page on datacircle.dev. That page lists every field. A tool also takes an API's or a provider's name where it asks for a slug.

When the API gives no data, like for a slug it doesn't know, `Call` and `Outputs` give the model `{"error": "..."}` with the API's own words and what to send instead, so the model can call again. A network failure is given the same way. `Run` gives both as its error.

`datacircle.Config` sets the `HTTPClient` the tools use (a timeout, a proxy). `nil` uses `http.DefaultClient`.

## Network

Each tool call makes one HTTPS GET request to https://api.datacircle.dev/v1 and nothing else. The request holds only the tool's own arguments: no key, no config, nothing from your chats. The API takes up to 60 calls a minute from one IP address. The tool waits once, for a minute at most, when the API asks the tool to wait. The module depends only on the OpenAI Go SDK and `golang.org/x/text`, and stores nothing.

## Links

- Datacircle: https://datacircle.dev
- The API: https://datacircle.dev/api
- The API's spec: https://api.datacircle.dev/v1/openapi.json
- What each key means: https://datacircle.dev/fields

MIT license.
