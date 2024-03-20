package openai

import (
	"bufio"
	"encoding/json"
	"fmt"

	fiberContext "github.com/go-skynet/LocalAI/core/http/ctx"
	"github.com/go-skynet/LocalAI/core/schema"
	"github.com/go-skynet/LocalAI/core/services"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
)

func ChatEndpoint(fce *fiberContext.FiberContextExtractor, oais *services.OpenAIService) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		_, request, err := fce.OpenAIRequestFromContext(c, false)
		if err != nil {
			return fmt.Errorf("failed reading parameters from request:%w", err)
		}

		// log.Debug().Msgf("`[CHAT] OpenAIRequest`: %+v", request)

		traceID, finalResultChannel, _, tokenChannel, err := oais.Chat(request, false, request.Stream)
		if err != nil {
			return err
		}

		if request.Stream {

			log.Debug().Msgf("Chat Stream request received")
			c.Context().SetContentType("text/event-stream")
			//c.Response().Header.SetContentType(fiber.MIMETextHTMLCharsetUTF8)
			//
			c.Set("Cache-Control", "no-cache")
			c.Set("Connection", "keep-alive")
			c.Set("Transfer-Encoding", "chunked")

			c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
				usage := &schema.OpenAIUsage{}
				toolsCalled := false
				for ev := range tokenChannel {
					if ev.Error != nil {
						log.Debug().Msgf("chat streaming responseChannel error: %q", ev.Error)
						request.Cancel()
						break
					}
					usage = &ev.Value.Usage // Copy a pointer to the latest usage chunk so that the stop message can reference it

					if len(ev.Value.Choices[0].Delta.ToolCalls) > 0 {
						toolsCalled = true
					}
					// var buf bytes.Buffer
					// enc := json.NewEncoder(&buf)
					// enc.Encode(ev.Value.Choices[0].Delta.Content)
					log.Debug().Msgf("chat streaming sending chunk: %s", ev.Value)
					_, err := fmt.Fprintf(w, "data: %v\n", ev.Value)
					if err != nil {
						log.Debug().Msgf("Sending chunk failed: %v", err)
						request.Cancel()
						break
					}
					err = w.Flush()
					if err != nil {
						log.Debug().Msg("error while flushing, closing connection")
						request.Cancel()
						break
					}
				}

				finishReason := "stop"
				if toolsCalled {
					finishReason = "tool_calls"
				} else if toolsCalled && len(request.Tools) == 0 {
					finishReason = "function_call"
				}

				resp := &schema.OpenAIResponse{
					ID:      traceID.ID,
					Created: traceID.Created,
					Model:   request.Model, // we have to return what the user sent here, due to OpenAI spec.
					Choices: []schema.Choice{
						{
							FinishReason: finishReason,
							Index:        0,
							Delta:        &schema.Message{Content: ""},
						}},
					Object: "chat.completion.chunk",
					Usage:  *usage,
				}
				respData, _ := json.Marshal(resp)

				w.WriteString(fmt.Sprintf("data: %s\n\n", respData))
				w.WriteString("data: [DONE]\n\n")
				w.Flush()
			}))

			return nil
		}

		// TODO is this proper to have exclusive from Stream, or do we need to issue both responses?
		rawResponse := <-finalResultChannel

		if rawResponse.Error != nil {
			return rawResponse.Error
		}

		jsonResult, _ := json.Marshal(rawResponse.Value)
		log.Debug().Msgf("Chat Final Response: %s", jsonResult)

		// Return the prediction in the response body
		return c.JSON(rawResponse.Value)
	}
}
