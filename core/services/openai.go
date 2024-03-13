package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-skynet/LocalAI/core/backend"
	"github.com/go-skynet/LocalAI/core/config"
	"github.com/go-skynet/LocalAI/core/schema"
	"github.com/go-skynet/LocalAI/pkg/grammar"
	"github.com/go-skynet/LocalAI/pkg/model"
	"github.com/go-skynet/LocalAI/pkg/utils"
	"github.com/google/uuid"
	"github.com/imdario/mergo"
	"github.com/rs/zerolog/log"
)

type endpointGenerationConfigurationFn func(bc *config.BackendConfig, request *schema.OpenAIRequest) (schemaObject string, templatePath string, templateData model.PromptTemplateData, mappingFn func(resp *backend.LLMResponse, index int) schema.Choice)

// TODO: Consider alternative names for this.
// The purpose of this struct is to hold a reference to the OpenAI request context information
// This keeps things simple within core/services/openai.go and allows consumers to "see" this information if they need it
type OpenAIRequestTraceID struct {
	ID      string
	Created int
}

// This type split out from core/backend/llm.go - I'm still not _totally_ sure about this, but it seems to make sense to keep the generic LLM code from the OpenAI specific higher level functionality
type OpenAIService struct {
	bcl       *config.BackendConfigLoader
	ml        *model.ModelLoader
	appConfig *config.ApplicationConfig
	llmbs     *backend.LLMBackendService
}

func NewOpenAIService(ml *model.ModelLoader, bcl *config.BackendConfigLoader, appConfig *config.ApplicationConfig, llmbs *backend.LLMBackendService) *OpenAIService {
	return &OpenAIService{
		bcl:       bcl,
		ml:        ml,
		appConfig: appConfig,
		llmbs:     llmbs,
	}
}

// Keeping in place as a reminder to POTENTIALLY ADD MORE VALIDATION HERE???
func (oais *OpenAIService) getConfig(request *schema.OpenAIRequest) (*config.BackendConfig, *schema.OpenAIRequest, error) {
	return config.LoadBackendConfigForModelAndOpenAIRequest(request.Model, request, oais.bcl, oais.appConfig)
}

// TODO: It would be a lot less messy to make a return struct that had references to each of these channels
// INTENTIONALLY not doing that quite yet - I believe we need to let the references to unused channels die for the GC to automatically collect -- can we manually free()?
// finalResultsChannel is the primary async return path: one result for the entire request.
// promptResultsChannels is DUBIOUS. It's expected to be raw fan-out used within the function itself, but I am exposing for testing? One bundle of LLMResponseBundle per PromptString? Gets all N completions for a single prompt.
// completionsChannel is a channel that emits one *LLMResponse per generated completion, be that different prompts or N. Seems the most useful other than "entire request" Request is available to attempt tracing???
// tokensChannel is a channel that emits one *LLMResponse per generated token. Let's see what happens!
func (oais *OpenAIService) Completion(request *schema.OpenAIRequest, notifyOnPromptResult bool, notifyOnToken bool) (
	traceID *OpenAIRequestTraceID, finalResultChannel <-chan utils.ErrorOr[*schema.OpenAIResponse], promptResultsChannels []<-chan utils.ErrorOr[*backend.LLMResponseBundle],
	completionsChannel <-chan utils.ErrorOr[*backend.LLMResponse], tokenChannel <-chan utils.ErrorOr[*backend.LLMResponse], err error) {

	return oais.GenerateTextFromRequest(request, func(bc *config.BackendConfig, request *schema.OpenAIRequest) (
		schemaObject string, templatePath string, templateData model.PromptTemplateData, mappingFn func(resp *backend.LLMResponse, promptIndex int) schema.Choice) {

		return "text_completion", bc.TemplateConfig.Completion, model.PromptTemplateData{
				SystemPrompt: bc.SystemPrompt,
			}, func(resp *backend.LLMResponse, promptIndex int) schema.Choice {
				log.Warn().Msgf("[oais.Completion] mappingFn input resp: %+v", resp)
				return schema.Choice{
					Index:        promptIndex,
					FinishReason: "stop",
					Text:         resp.Response,
				}
			}
	}, notifyOnPromptResult, notifyOnToken, nil)
}

func (oais *OpenAIService) Edit(request *schema.OpenAIRequest, notifyOnPromptResult bool, notifyOnToken bool) (
	traceID *OpenAIRequestTraceID, finalResultChannel <-chan utils.ErrorOr[*schema.OpenAIResponse], promptResultsChannels []<-chan utils.ErrorOr[*backend.LLMResponseBundle],
	completionsChannel <-chan utils.ErrorOr[*backend.LLMResponse], tokenChannel <-chan utils.ErrorOr[*backend.LLMResponse], err error) {

	return oais.GenerateTextFromRequest(request, func(bc *config.BackendConfig, request *schema.OpenAIRequest) (
		schemaObject string, templatePath string, templateData model.PromptTemplateData, mappingFn func(resp *backend.LLMResponse, promptIndex int) schema.Choice) {

		return "edit", bc.TemplateConfig.Edit, model.PromptTemplateData{
				SystemPrompt: bc.SystemPrompt,
				Instruction:  request.Instruction,
			}, func(resp *backend.LLMResponse, promptIndex int) schema.Choice {
				return schema.Choice{
					Index:        promptIndex,
					FinishReason: "stop",
					Text:         resp.Response,
				}
			}
	}, notifyOnPromptResult, notifyOnToken, nil)
}

func (oais *OpenAIService) Chat(request *schema.OpenAIRequest, notifyOnPromptResult bool, notifyOnToken bool) (
	traceID *OpenAIRequestTraceID, finalResultChannel <-chan utils.ErrorOr[*schema.OpenAIResponse],
	completionsChannel <-chan utils.ErrorOr[*backend.LLMResponse], tokenChannel <-chan utils.ErrorOr[*backend.LLMResponse], err error) {

	return oais.GenerateFromMultipleMessagesChatRequest(request, notifyOnPromptResult, notifyOnToken, nil)
}

func (oais *OpenAIService) GenerateTextFromRequest(request *schema.OpenAIRequest, endpointConfigFn endpointGenerationConfigurationFn, notifyOnPromptResult bool, notifyOnToken bool, initialTraceID *OpenAIRequestTraceID) (
	traceID *OpenAIRequestTraceID, finalResultChannel <-chan utils.ErrorOr[*schema.OpenAIResponse], promptResultsChannels []<-chan utils.ErrorOr[*backend.LLMResponseBundle],
	completionsChannel <-chan utils.ErrorOr[*backend.LLMResponse], tokenChannel <-chan utils.ErrorOr[*backend.LLMResponse], err error) {

	if initialTraceID == nil {
		traceID = &OpenAIRequestTraceID{
			ID:      uuid.New().String(),
			Created: int(time.Now().Unix()),
		}
	} else {
		traceID = initialTraceID
	}

	bc, request, err := oais.getConfig(request)
	if err != nil {
		log.Error().Msgf("[oais::GenerateTextFromRequest] error getting configuration: %q", err)
		return
	}

	if request.ResponseFormat.Type == "json_object" {
		request.Grammar = grammar.JSONBNF
	}

	if request.Stream && len(bc.PromptStrings) > 1 {
		log.Warn().Msg("potentially cannot handle more than 1 `PromptStrings` when Streaming?")
	}

	rawFinalResultChannel := make(chan utils.ErrorOr[*schema.OpenAIResponse])
	promptResultsChannels = []<-chan utils.ErrorOr[*backend.LLMResponseBundle]{}
	var rawCompletionsChannel chan utils.ErrorOr[*backend.LLMResponse]
	var rawTokenChannel chan utils.ErrorOr[*backend.LLMResponse]
	if notifyOnPromptResult {
		rawCompletionsChannel = make(chan utils.ErrorOr[*backend.LLMResponse])
	}
	if notifyOnToken {
		rawTokenChannel = make(chan utils.ErrorOr[*backend.LLMResponse])
	}

	promptResultsChannelLock := sync.Mutex{}

	schemaObject, templateFile, commonPromptData, mappingFn := endpointConfigFn(bc, request)

	if len(templateFile) == 0 {
		// A model can have a "file.bin.tmpl" file associated with a prompt template prefix
		if oais.ml.ExistsInModelPath(fmt.Sprintf("%s.tmpl", bc.Model)) {
			templateFile = bc.Model
		} else {
			log.Warn().Msgf("failed to find any template for %+v", request)
		}
	}

	setupWG := sync.WaitGroup{}
	setupWG.Add(len(bc.PromptStrings))

	for pI, p := range bc.PromptStrings {

		go func(promptIndex int, prompt string) {
			if templateFile != "" {
				promptTemplateData := model.PromptTemplateData{
					Input: prompt,
				}
				err := mergo.Merge(promptTemplateData, commonPromptData, mergo.WithOverride)
				if err == nil {
					templatedInput, err := oais.ml.EvaluateTemplateForPrompt(model.CompletionPromptTemplate, templateFile, promptTemplateData)
					if err == nil {
						prompt = templatedInput
						log.Debug().Msgf("Template found, input modified to: %s", prompt)
					}
				}
			}

			log.Debug().Msgf("[OAIS GenerateTextFromRequest] Prompt: %q", prompt)
			promptResultsChannel, completionChannels, tokenChannels, err := oais.llmbs.GenerateText(prompt, request, bc,
				func(r *backend.LLMResponse) schema.Choice {
					log.Debug().Msgf("[OAIS GenerateTextFromRequest::GenerateText::MAPFN] %d => %+v", promptIndex, r)
					return mappingFn(r, promptIndex)
				}, false, notifyOnToken)
			if err != nil {
				log.Error().Msgf("TODO DEBUG IF HIT:\nprompt: %q\nerr: %q", prompt, err)
				return
			}
			if notifyOnPromptResult {
				utils.SliceOfChannelsRawMergerWithoutMapping(completionChannels, rawCompletionsChannel, true)
			}
			if notifyOnToken {
				utils.SliceOfChannelsRawMergerWithoutMapping(tokenChannels, rawTokenChannel, true)
			}
			promptResultsChannelLock.Lock()
			promptResultsChannels = append(promptResultsChannels, promptResultsChannel)
			promptResultsChannelLock.Unlock()
			setupWG.Done()
		}(pI, p)

	}
	setupWG.Wait()

	initialResponse := &schema.OpenAIResponse{
		ID:      traceID.ID,
		Created: traceID.Created,
		Model:   request.Model,
		Object:  schemaObject,
		Usage:   schema.OpenAIUsage{},
	}

	// utils.SliceOfChannelsRawMerger[[]schema.Choice](promptResultsChannels, rawFinalResultChannel, func(results []schema.Choice) (*schema.OpenAIResponse, error) {
	utils.SliceOfChannelsReducer[utils.ErrorOr[*backend.LLMResponseBundle], utils.ErrorOr[*schema.OpenAIResponse]](
		promptResultsChannels, rawFinalResultChannel,
		func(iv utils.ErrorOr[*backend.LLMResponseBundle], result utils.ErrorOr[*schema.OpenAIResponse]) utils.ErrorOr[*schema.OpenAIResponse] {

			if iv.Error != nil {
				result.Error = iv.Error
				return result
			}
			result.Value.Usage.PromptTokens += iv.Value.Usage.Prompt
			result.Value.Usage.CompletionTokens += iv.Value.Usage.Completion
			result.Value.Usage.TotalTokens = result.Value.Usage.PromptTokens + result.Value.Usage.CompletionTokens

			result.Value.Choices = append(result.Value.Choices, iv.Value.Response...)

			log.Warn().Msgf("[oais::GenerateTextFromRequest] REDUCER Choices: %+v", result.Value.Choices)

			return result
		}, utils.ErrorOr[*schema.OpenAIResponse]{Value: initialResponse}, true)

	finalResultChannel = rawFinalResultChannel
	completionsChannel = rawCompletionsChannel
	tokenChannel = rawTokenChannel

	return
}

// TODO: For porting sanity, this is distinct from GenerateTextFromRequest and is _currently_ specific to Chat purposes
// this is not a final decision -- just a reality of moving a lot of parts at once
// / This has _become_ Chat which wasn't the goal... More cleanup in the future once it's stable?
func (oais *OpenAIService) GenerateFromMultipleMessagesChatRequest(request *schema.OpenAIRequest, notifyOnPromptResult bool, notifyOnToken bool, initialTraceID *OpenAIRequestTraceID) (
	traceID *OpenAIRequestTraceID, finalResultChannel <-chan utils.ErrorOr[*schema.OpenAIResponse],
	completionsChannel <-chan utils.ErrorOr[*backend.LLMResponse], tokenChannel <-chan utils.ErrorOr[*backend.LLMResponse], err error) {

	if initialTraceID == nil {
		traceID = &OpenAIRequestTraceID{
			ID:      uuid.New().String(),
			Created: int(time.Now().Unix()),
		}
	} else {
		traceID = initialTraceID
	}

	bc, request, err := oais.getConfig(request)
	if err != nil {
		return
	}

	// Allow the user to set custom actions via config file
	// to be "embedded" in each model
	noActionName := "answer"
	noActionDescription := "use this action to answer without performing any action"

	if bc.FunctionsConfig.NoActionFunctionName != "" {
		noActionName = bc.FunctionsConfig.NoActionFunctionName
	}
	if bc.FunctionsConfig.NoActionDescriptionName != "" {
		noActionDescription = bc.FunctionsConfig.NoActionDescriptionName
	}

	if request.ResponseFormat.Type == "json_object" {
		request.Grammar = grammar.JSONBNF
	}

	processFunctions := false
	funcs := grammar.Functions{}
	// process functions if we have any defined or if we have a function call string
	if len(request.Functions) > 0 && bc.ShouldUseFunctions() {
		log.Debug().Msgf("Response needs to process functions")

		processFunctions = true

		noActionGrammar := grammar.Function{
			Name:        noActionName,
			Description: noActionDescription,
			Parameters: map[string]interface{}{
				"properties": map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The message to reply the user with",
					}},
			},
		}

		// Append the no action function
		funcs = append(funcs, request.Functions...)
		if !bc.FunctionsConfig.DisableNoAction {
			funcs = append(funcs, noActionGrammar)
		}

		// Force picking one of the functions by the request
		if bc.FunctionToCall() != "" {
			funcs = funcs.Select(bc.FunctionToCall())
		}

		// Update input grammar
		jsStruct := funcs.ToJSONStructure()
		bc.Grammar = jsStruct.Grammar("", bc.FunctionsConfig.ParallelCalls)
	} else if request.JSONFunctionGrammarObject != nil {
		bc.Grammar = request.JSONFunctionGrammarObject.Grammar("", bc.FunctionsConfig.ParallelCalls)
	}

	if request.Stream && processFunctions {
		log.Warn().Msg("Streaming + Functions is highly experimental in this version")
	}

	var predInput string

	suppressConfigSystemPrompt := false
	mess := []string{}
	for messageIndex, i := range request.Messages {
		var content string
		role := i.Role

		// if function call, we might want to customize the role so we can display better that the "assistant called a json action"
		// if an "assistant_function_call" role is defined, we use it, otherwise we use the role that is passed by in the request
		if i.FunctionCall != nil && i.Role == "assistant" {
			roleFn := "assistant_function_call"
			r := bc.Roles[roleFn]
			if r != "" {
				role = roleFn
			}
		}
		r := bc.Roles[role]
		contentExists := i.Content != nil && i.StringContent != ""

		// First attempt to populate content via a chat message specific template
		if bc.TemplateConfig.ChatMessage != "" {
			chatMessageData := model.ChatMessageTemplateData{
				SystemPrompt: bc.SystemPrompt,
				Role:         r,
				RoleName:     role,
				Content:      i.StringContent,
				FunctionName: i.Name,
				MessageIndex: messageIndex,
			}
			templatedChatMessage, err := oais.ml.EvaluateTemplateForChatMessage(bc.TemplateConfig.ChatMessage, chatMessageData)
			if err != nil {
				log.Error().Msgf("error processing message %+v using template \"%s\": %v. Skipping!", chatMessageData, bc.TemplateConfig.ChatMessage, err)
			} else {
				if templatedChatMessage == "" {
					log.Warn().Msgf("template \"%s\" produced blank output for %+v. Skipping!", bc.TemplateConfig.ChatMessage, chatMessageData)
					continue // TODO: This continue is here intentionally to skip over the line `mess = append(mess, content)` below, and to prevent the sprintf
				}
				log.Debug().Msgf("templated message for chat: %s", templatedChatMessage)
				content = templatedChatMessage
			}
		}
		// If this model doesn't have such a template, or if that template fails to return a value, template at the message level.
		if content == "" {
			if r != "" {
				if contentExists {
					content = fmt.Sprint(r, i.StringContent)
				}
				if i.FunctionCall != nil {
					j, err := json.Marshal(i.FunctionCall)
					if err == nil {
						if contentExists {
							content += "\n" + fmt.Sprint(r, " ", string(j))
						} else {
							content = fmt.Sprint(r, " ", string(j))
						}
					}
				}
			} else {
				if contentExists {
					content = fmt.Sprint(i.StringContent)
				}
				if i.FunctionCall != nil {
					j, err := json.Marshal(i.FunctionCall)
					if err == nil {
						if contentExists {
							content += "\n" + string(j)
						} else {
							content = string(j)
						}
					}
				}
			}
			// Special Handling: System. We care if it was printed at all, not the r branch, so check seperately
			if contentExists && role == "system" {
				suppressConfigSystemPrompt = true
			}
		}

		mess = append(mess, content)
	}

	predInput = strings.Join(mess, "\n")
	log.Debug().Msgf("Prompt (before templating): %s", predInput)

	templateFile := ""
	// A model can have a "file.bin.tmpl" file associated with a prompt template prefix
	if oais.ml.ExistsInModelPath(fmt.Sprintf("%s.tmpl", bc.Model)) {
		templateFile = bc.Model
	}

	if bc.TemplateConfig.Chat != "" && !processFunctions {
		templateFile = bc.TemplateConfig.Chat
	}

	if bc.TemplateConfig.Functions != "" && processFunctions {
		templateFile = bc.TemplateConfig.Functions
	}

	if templateFile != "" {
		templatedInput, err := oais.ml.EvaluateTemplateForPrompt(model.ChatPromptTemplate, templateFile, model.PromptTemplateData{
			SystemPrompt:         bc.SystemPrompt,
			SuppressSystemPrompt: suppressConfigSystemPrompt,
			Input:                predInput,
			Functions:            funcs,
		})
		if err == nil {
			predInput = templatedInput
			log.Debug().Msgf("Template found, input modified to: %s", predInput)
		} else {
			log.Debug().Msgf("Template failed loading: %s", err.Error())
		}
	}

	log.Debug().Msgf("Prompt (after templating): %s", predInput)
	if processFunctions {
		log.Debug().Msgf("Grammar: %+v", bc.Grammar)
	}

	rawFinalResultChannel := make(chan utils.ErrorOr[*schema.OpenAIResponse])
	// promptResultsChannels = []<-chan utils.ErrorOr[*backend.LLMResponseBundle]{}
	var rawCompletionsChannel chan utils.ErrorOr[*backend.LLMResponse]
	var rawTokenChannel chan utils.ErrorOr[*backend.LLMResponse]
	if notifyOnPromptResult {
		rawCompletionsChannel = make(chan utils.ErrorOr[*backend.LLMResponse])
	}
	if notifyOnToken {
		rawTokenChannel = make(chan utils.ErrorOr[*backend.LLMResponse])
	}

	rawResultChannel, individualCompletionChannels, tokenChannels, err := oais.llmbs.GenerateText(predInput, request, bc, func(resp *backend.LLMResponse) schema.Choice {
		return schema.Choice{
			Index:        0, // ???
			FinishReason: "stop",
			Message: &schema.Message{
				Role:    "assistant",
				Content: resp.Response,
			},
		}
	}, notifyOnPromptResult, notifyOnToken)

	if notifyOnPromptResult {
		utils.SliceOfChannelsRawMergerWithoutMapping(individualCompletionChannels, rawCompletionsChannel, true)
	}
	if notifyOnToken {
		utils.SliceOfChannelsRawMergerWithoutMapping(tokenChannels, rawTokenChannel, true)
	}

	go func() {
		rawResult := <-rawResultChannel
		if rawResult.Error != nil {
			log.Warn().Msgf("OpenAIService::processTools GenerateText error [DEBUG THIS?] %q", rawResult.Error)
			return
		}
		llmResponseChoices := rawResult.Value.Response

		if processFunctions && len(llmResponseChoices) > 1 {
			log.Warn().Msgf("chat functions response with %d choices in response, debug this?", len(llmResponseChoices))
			log.Debug().Msgf("%+v", llmResponseChoices)
		}

		for _, result := range rawResult.Value.Response {

			// log.Warn().Msgf("[OAIS GenerateFromMultipleMessagesChatRequest] rawResult.Value.Response: %+v", result)
			// If no functions, just return the raw result.
			if !processFunctions {

				resp := schema.OpenAIResponse{
					ID:      traceID.ID,
					Created: traceID.Created,
					Model:   request.Model, // we have to return what the user sent here, due to OpenAI spec.
					Choices: []schema.Choice{result},
					Object:  "chat.completion.chunk",
					Usage: schema.OpenAIUsage{
						PromptTokens:     rawResult.Value.Usage.Prompt,
						CompletionTokens: rawResult.Value.Usage.Completion,
						TotalTokens:      rawResult.Value.Usage.Prompt + rawResult.Value.Usage.Prompt,
					},
				}

				// log.Warn().Msgf("[OAIS GenerateFromMultipleMessagesChatRequest]\n\nresp.Choices[0].Message.Content: %+v\n\nresp.Choices[0].Text: %+v", resp.Choices[0].Message.Content, resp.Choices[0].Text)

				rawFinalResultChannel <- utils.ErrorOr[*schema.OpenAIResponse]{Value: &resp}

				continue
			}
			// At this point, things are function specific!
			results := parseFunctionCall(result.Text, bc.FunctionsConfig.ParallelCalls)
			noActionToRun := (len(results) > 0 && results[0].name == noActionName)

			if noActionToRun {
				log.Debug().Msg("-- noActionToRun branch --")
				initialMessage := schema.OpenAIResponse{
					ID:      traceID.ID,
					Created: traceID.Created,
					Model:   request.Model, // we have to return what the user sent here, due to OpenAI spec.
					Choices: []schema.Choice{{Delta: &schema.Message{Role: "assistant", Content: ""}}},
					Object:  "chat.completion.chunk",
				}
				rawFinalResultChannel <- utils.ErrorOr[*schema.OpenAIResponse]{Value: &initialMessage}

				log.Debug().Msg("[noActionToRun::rawFinalResultChannel] initial message consumed")

				// args := results[0].arguments

				result, err := oais.handleQuestion(bc, request, results[0].arguments, predInput)
				if err != nil {
					log.Error().Msgf("error handling question: %s", err.Error())
					return
				}

				resp := schema.OpenAIResponse{
					ID:      traceID.ID,
					Created: traceID.Created,
					Model:   request.Model, // we have to return what the user sent here, due to OpenAI spec.
					Choices: []schema.Choice{{Delta: &schema.Message{Content: &result}, Index: 0}},
					Object:  "chat.completion.chunk",
					Usage: schema.OpenAIUsage{
						PromptTokens:     rawResult.Value.Usage.Prompt,
						CompletionTokens: rawResult.Value.Usage.Completion,
						TotalTokens:      rawResult.Value.Usage.Prompt + rawResult.Value.Usage.Prompt,
					},
				}

				rawFinalResultChannel <- utils.ErrorOr[*schema.OpenAIResponse]{Value: &resp}

			} else {
				log.Warn().Msgf("[GenerateFromMultipleMessagesChatRequest] fnResultsBranch: %+v", results)
				for i, ss := range results {
					name, args := ss.name, ss.arguments

					initialMessage := schema.OpenAIResponse{
						ID:      traceID.ID,
						Created: traceID.Created,
						Model:   request.Model, // we have to return what the user sent here, due to OpenAI spec.
						Choices: []schema.Choice{{
							Delta: &schema.Message{
								Role: "assistant",
								ToolCalls: []schema.ToolCall{
									{
										Index: i,
										ID:    traceID.ID,
										Type:  "function",
										FunctionCall: schema.FunctionCall{
											Name: name,
										},
									},
								},
							}}},
						Object: "chat.completion.chunk",
					}
					rawFinalResultChannel <- utils.ErrorOr[*schema.OpenAIResponse]{Value: &initialMessage}

					rawFinalResultChannel <- utils.ErrorOr[*schema.OpenAIResponse]{Value: &schema.OpenAIResponse{
						ID:      traceID.ID,
						Created: traceID.Created,
						Model:   request.Model, // we have to return what the user sent here, due to OpenAI spec.
						Choices: []schema.Choice{{
							Delta: &schema.Message{
								Role: "assistant",
								ToolCalls: []schema.ToolCall{
									{
										Index: i,
										ID:    traceID.ID,
										Type:  "function",
										FunctionCall: schema.FunctionCall{
											Arguments: args,
										},
									},
								},
							}}},
						Object: "chat.completion.chunk",
					}}
				}
			}
		}

		close(rawFinalResultChannel)
	}()

	finalResultChannel = rawFinalResultChannel
	completionsChannel = rawCompletionsChannel
	tokenChannel = rawTokenChannel
	return
}

func (oais *OpenAIService) handleQuestion(config *config.BackendConfig, input *schema.OpenAIRequest, args, prompt string) (string, error) {
	log.Debug().Msgf("[handleQuestion called] nothing to do, computing a reply")

	// If there is a message that the LLM already sends as part of the JSON reply, use it
	arguments := map[string]interface{}{}
	json.Unmarshal([]byte(args), &arguments)
	m, exists := arguments["message"]
	if exists {
		switch message := m.(type) {
		case string:
			if message != "" {
				log.Debug().Msgf("Reply received from LLM: %s", message)
				message = oais.llmbs.Finetune(*config, prompt, message)
				log.Debug().Msgf("Reply received from LLM(finetuned): %s", message)

				return message, nil
			}
		}
	}

	log.Debug().Msgf("No action received from LLM, without a message, computing a reply")
	// Otherwise ask the LLM to understand the JSON output and the context, and return a message
	// Note: This costs (in term of CPU/GPU) another computation
	config.Grammar = ""
	images := []string{}
	for _, m := range input.Messages {
		images = append(images, m.StringImages...)
	}

	resultChannel, _, err := oais.llmbs.Inference(input.Context, &backend.LLMRequest{
		Text:   prompt,
		Images: images,
	}, config, false)

	if err != nil {
		log.Error().Msgf("inference setup error: %s", err.Error())
		return "", err
	}

	raw := <-resultChannel
	if raw.Error != nil {
		log.Error().Msgf("inference error: %q", raw.Error.Error())
		return "", err
	}
	if raw.Value == nil {
		log.Warn().Msgf("nil inference response")
		return "", nil
	}
	return oais.llmbs.Finetune(*config, prompt, raw.Value.Response), nil
}

type funcCallResults struct {
	name      string
	arguments string
}

func parseFunctionCall(llmresult string, multipleResults bool) []funcCallResults {
	results := []funcCallResults{}

	// TODO: use generics to avoid this code duplication
	if multipleResults {
		ss := []map[string]interface{}{}
		s := utils.EscapeNewLines(llmresult)
		json.Unmarshal([]byte(s), &ss)
		log.Debug().Msgf("[if] Function return: %s %+v", s, ss)

		for _, s := range ss {
			func_name, ok := s["function"]
			if !ok {
				continue
			}
			args, ok := s["arguments"]
			if !ok {
				continue
			}
			d, _ := json.Marshal(args)
			funcName, ok := func_name.(string)
			if !ok {
				continue
			}
			results = append(results, funcCallResults{name: funcName, arguments: string(d)})
		}
	} else {
		// As we have to change the result before processing, we can't stream the answer token-by-token (yet?)
		ss := map[string]interface{}{}
		// This prevent newlines to break JSON parsing for clients
		s := utils.EscapeNewLines(llmresult)
		json.Unmarshal([]byte(s), &ss)
		log.Debug().Msgf("[else] Function return: %s %+v", s, ss)

		// The grammar defines the function name as "function", while OpenAI returns "name"
		func_name, ok := ss["function"]
		if !ok {
			log.Debug().Msg("ss[function] is not OK!")
			return results
		}
		// Similarly, while here arguments is a map[string]interface{}, OpenAI actually want a stringified object
		args, ok := ss["arguments"] // arguments needs to be a string, but we return an object from the grammar result (TODO: fix)
		if !ok {
			log.Debug().Msg("ss[arguments] is not OK!")
			return results
		}
		d, _ := json.Marshal(args)
		funcName, ok := func_name.(string)
		if !ok {
			log.Debug().Msgf("unexpected func_name: %+v", func_name)
			return results
		}
		results = append(results, funcCallResults{name: funcName, arguments: string(d)})
	}
	return results
}

// TODO RENAME THIS POST REFACTOR OBVIOUSLY!!!!!
// func (oais *OpenAIService) oldProcessFunc(traceID *OpenAIRequestTraceID, s string, req *schema.OpenAIRequest, config *config.BackendConfig, loader *model.ModelLoader, responses chan schema.OpenAIResponse) {
// 	initialMessage := schema.OpenAIResponse{
// 		ID:      traceID.ID,
// 		Created: traceID.Created,
// 		Model:   req.Model, // we have to return what the user sent here, due to OpenAI spec.
// 		Choices: []schema.Choice{{Delta: &schema.Message{Role: "assistant", Content: ""}}},
// 		Object:  "chat.completion.chunk",
// 	}
// 	responses <- initialMessage

// 	ComputeChoices(req, s, config, startupOptions, loader, func(s string, c *[]schema.Choice) {}, func(s string, usage backend.TokenUsage) bool {
// 		resp := schema.OpenAIResponse{
// 			ID:      id,
// 			Created: created,
// 			Model:   req.Model, // we have to return what the user sent here, due to OpenAI spec.
// 			Choices: []schema.Choice{{Delta: &schema.Message{Content: &s}, Index: 0}},
// 			Object:  "chat.completion.chunk",
// 			Usage: schema.OpenAIUsage{
// 				PromptTokens:     usage.Prompt,
// 				CompletionTokens: usage.Completion,
// 				TotalTokens:      usage.Prompt + usage.Completion,
// 			},
// 		}

// 		responses <- resp
// 		return true
// 	})
// 	close(responses)
// }
