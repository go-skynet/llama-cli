package http

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/go-skynet/LocalAI/core/http/endpoints/localai"
	"github.com/go-skynet/LocalAI/core/http/endpoints/openai"

	"github.com/go-skynet/LocalAI/core/config"
	"github.com/go-skynet/LocalAI/core/schema"
	"github.com/go-skynet/LocalAI/core/services"
	"github.com/go-skynet/LocalAI/internal"
	"github.com/go-skynet/LocalAI/pkg/model"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func App(cl *config.BackendConfigLoader, ml *model.ModelLoader, options *config.ApplicationConfig) (*fiber.App, error) {
	// Return errors as JSON responses
	app := fiber.New(fiber.Config{
		BodyLimit:             options.UploadLimitMB * 1024 * 1024, // this is the default limit of 4MB
		DisableStartupMessage: options.DisableMessage,
		// Override default error handler
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			// Status code defaults to 500
			code := fiber.StatusInternalServerError

			// Retrieve the custom status code if it's a *fiber.Error
			var e *fiber.Error
			if errors.As(err, &e) {
				code = e.Code
			}

			// Send custom error page
			return ctx.Status(code).JSON(
				schema.ErrorResponse{
					Error: &schema.APIError{Message: err.Error(), Code: code},
				},
			)
		},
	})

	if options.Debug {
		app.Use(logger.New(logger.Config{
			Format: "[${ip}]:${port} ${status} - ${method} ${path}\n",
		}))
	}

	// Default middleware config

	if !options.Debug {
		app.Use(recover.New())
	}

	metricsService, err := services.NewLocalAiMetricsService()
	if err != nil {
		return nil, err
	}

	if metricsService != nil {
		app.Use(localai.LocalAiMetricsAPIMiddleware(metricsService))
		app.Hooks().OnShutdown(func() error {
			return metricsService.Shutdown()
		})
	}

	// Auth middleware checking if API key is valid. If no API key is set, no auth is required.
	auth := func(c *fiber.Ctx) error {
		if len(options.ApiKeys) == 0 {
			return c.Next()
		}

		// Check for api_keys.json file
		fileContent, err := os.ReadFile("api_keys.json")
		if err == nil {
			// Parse JSON content from the file
			var fileKeys []string
			err := json.Unmarshal(fileContent, &fileKeys)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Error parsing api_keys.json"})
			}

			// Add file keys to options.ApiKeys
			options.ApiKeys = append(options.ApiKeys, fileKeys...)
		}

		if len(options.ApiKeys) == 0 {
			return c.Next()
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"message": "Authorization header missing"})
		}
		authHeaderParts := strings.Split(authHeader, " ")
		if len(authHeaderParts) != 2 || authHeaderParts[0] != "Bearer" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"message": "Invalid Authorization header format"})
		}

		apiKey := authHeaderParts[1]
		for _, key := range options.ApiKeys {
			if apiKey == key {
				return c.Next()
			}
		}

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"message": "Invalid API key"})

	}

	if options.CORS {
		var c func(ctx *fiber.Ctx) error
		if options.CORSAllowOrigins == "" {
			c = cors.New()
		} else {
			c = cors.New(cors.Config{AllowOrigins: options.CORSAllowOrigins})
		}

		app.Use(c)
	}

	// LocalAI API endpoints
	galleryService := services.NewGalleryService(options.ModelPath)
	galleryService.Start(options.Context, cl)

	app.Get("/version", auth, func(c *fiber.Ctx) error {
		return c.JSON(struct {
			Version string `json:"version"`
		}{Version: internal.PrintableVersion()})
	})

	// Make sure directories exists
	os.MkdirAll(options.ImageDir, 0755)
	os.MkdirAll(options.AudioDir, 0755)
	os.MkdirAll(options.UploadDir, 0755)
	os.MkdirAll(options.ModelPath, 0755)

	// Load upload json
	openai.LoadUploadConfig(options.UploadDir)

	modelGalleryEndpointService := localai.CreateModelGalleryEndpointService(options.Galleries, options.ModelPath, galleryService)
	app.Post("/models/apply", auth, modelGalleryEndpointService.ApplyModelGalleryEndpoint())
	app.Get("/models/available", auth, modelGalleryEndpointService.ListModelFromGalleryEndpoint())
	app.Get("/models/galleries", auth, modelGalleryEndpointService.ListModelGalleriesEndpoint())
	app.Post("/models/galleries", auth, modelGalleryEndpointService.AddModelGalleryEndpoint())
	app.Delete("/models/galleries", auth, modelGalleryEndpointService.RemoveModelGalleryEndpoint())
	app.Get("/models/jobs/:uuid", auth, modelGalleryEndpointService.GetOpStatusEndpoint())
	app.Get("/models/jobs", auth, modelGalleryEndpointService.GetAllStatusEndpoint())

	// openAI compatible API endpoint

	// chat
	app.Post("/v1/chat/completions", auth, openai.ChatEndpoint(cl, ml, options))
	app.Post("/chat/completions", auth, openai.ChatEndpoint(cl, ml, options))

	// edit
	app.Post("/v1/edits", auth, openai.EditEndpoint(cl, ml, options))
	app.Post("/edits", auth, openai.EditEndpoint(cl, ml, options))

	// files
	app.Post("/v1/files", auth, openai.UploadFilesEndpoint(cl, options))
	app.Post("/files", auth, openai.UploadFilesEndpoint(cl, options))
	app.Get("/v1/files", auth, openai.ListFilesEndpoint(cl, options))
	app.Get("/files", auth, openai.ListFilesEndpoint(cl, options))
	app.Get("/v1/files/:file_id", auth, openai.GetFilesEndpoint(cl, options))
	app.Get("/files/:file_id", auth, openai.GetFilesEndpoint(cl, options))
	app.Delete("/v1/files/:file_id", auth, openai.DeleteFilesEndpoint(cl, options))
	app.Delete("/files/:file_id", auth, openai.DeleteFilesEndpoint(cl, options))
	app.Get("/v1/files/:file_id/content", auth, openai.GetFilesContentsEndpoint(cl, options))
	app.Get("/files/:file_id/content", auth, openai.GetFilesContentsEndpoint(cl, options))

	// completion
	app.Post("/v1/completions", auth, openai.CompletionEndpoint(cl, ml, options))
	app.Post("/completions", auth, openai.CompletionEndpoint(cl, ml, options))
	app.Post("/v1/engines/:model/completions", auth, openai.CompletionEndpoint(cl, ml, options))

	// embeddings
	app.Post("/v1/embeddings", auth, openai.EmbeddingsEndpoint(cl, ml, options))
	app.Post("/embeddings", auth, openai.EmbeddingsEndpoint(cl, ml, options))
	app.Post("/v1/engines/:model/embeddings", auth, openai.EmbeddingsEndpoint(cl, ml, options))

	// audio
	app.Post("/v1/audio/transcriptions", auth, openai.TranscriptEndpoint(cl, ml, options))
	app.Post("/tts", auth, localai.TTSEndpoint(cl, ml, options))

	// images
	app.Post("/v1/images/generations", auth, openai.ImageEndpoint(cl, ml, options))

	if options.ImageDir != "" {
		app.Static("/generated-images", options.ImageDir)
	}

	if options.AudioDir != "" {
		app.Static("/generated-audio", options.AudioDir)
	}

	ok := func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	}

	// Kubernetes health checks
	app.Get("/healthz", ok)
	app.Get("/readyz", ok)

	// Experimental Backend Statistics Module
	backendMonitor := services.NewBackendMonitor(cl, ml, options) // Split out for now
	app.Get("/backend/monitor", localai.BackendMonitorEndpoint(backendMonitor))
	app.Post("/backend/shutdown", localai.BackendShutdownEndpoint(backendMonitor))

	// models
	app.Get("/v1/models", auth, openai.ListModelsEndpoint(cl, ml))
	app.Get("/models", auth, openai.ListModelsEndpoint(cl, ml))

	app.Get("/metrics", localai.LocalAiMetricsEndpoint())

	return app, nil
}
