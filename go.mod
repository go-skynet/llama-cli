module github.com/go-skynet/LocalAI

go 1.19

require (
	github.com/deepmap/oapi-codegen v0.0.0-00010101000000-000000000000
	github.com/donomii/go-rwkv.cpp v0.0.0-20230515123100-6fdd0c338e56
	github.com/ggerganov/whisper.cpp/bindings/go v0.0.0-20230520182345-041be06d5881
	github.com/go-audio/wav v1.1.0
	github.com/go-skynet/bloomz.cpp v0.0.0-20230510223001-e9366e82abdf
	github.com/go-skynet/go-bert.cpp v0.0.0-20230516063724-cea1ed76a7f4
	github.com/go-skynet/go-gpt2.cpp v0.0.0-20230512145559-7bff56f02245
	github.com/go-skynet/go-llama.cpp v0.0.0-20230520155239-ccf23adfb278
	github.com/gofiber/fiber/v2 v2.46.0
	github.com/google/uuid v1.3.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/imdario/mergo v0.3.15
	github.com/mudler/go-stable-diffusion v0.0.0-20230516152536-c0748eca3642
	github.com/nomic-ai/gpt4all/gpt4all-bindings/golang v0.0.0-20230519014017-914519e772fd
	github.com/onsi/ginkgo/v2 v2.9.5
	github.com/onsi/gomega v1.27.7
	github.com/otiai10/openaigo v1.1.0
	github.com/rs/zerolog v1.29.1
	github.com/sashabaranov/go-openai v1.9.4
	github.com/urfave/cli/v2 v2.25.3
	github.com/valyala/fasthttp v1.47.0
	github.com/vmware-tanzu/carvel-ytt v0.45.1
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/BurntSushi/toml v1.2.1 // indirect
	github.com/andybalholm/brotli v1.0.5 // indirect
	github.com/cppforlife/cobrautil v0.0.0-20200514214827-bb86e6965d72 // indirect
	github.com/cppforlife/go-cli-ui v0.0.0-20200505234325-512793797f05 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/getkin/kin-openapi v0.116.0 // indirect
	github.com/go-audio/audio v1.0.0 // indirect
	github.com/go-audio/riff v1.0.0 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/swag v0.21.1 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/pprof v0.0.0-20210407192527-94a9f03dee38 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-version v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/invopop/yaml v0.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/k14s/starlark-go v0.0.0-20200720175618-3a5c849cc368 // indirect
	github.com/klauspost/compress v1.16.3 // indirect
	github.com/labstack/echo/v4 v4.10.2 // indirect
	github.com/labstack/gommon v0.4.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.18 // indirect
	github.com/mattn/go-runewidth v0.0.14 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/otiai10/mint v1.5.1 // indirect
	github.com/perimeterx/marshmallow v1.1.4 // indirect
	github.com/philhofer/fwd v1.1.2 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/savsgio/dictpool v0.0.0-20221023140959-7bf2e61cea94 // indirect
	github.com/savsgio/gotils v0.0.0-20230208104028-c358bd845dee // indirect
	github.com/spf13/cobra v1.6.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/tinylib/msgp v1.1.8 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	golang.org/x/crypto v0.7.0 // indirect
	golang.org/x/mod v0.10.0 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sys v0.8.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	golang.org/x/tools v0.9.1 // indirect
)

replace github.com/go-skynet/go-llama.cpp => /workspace/go-llama

replace github.com/nomic-ai/gpt4all/gpt4all-bindings/golang => /workspace/gpt4all/gpt4all-bindings/golang

replace github.com/go-skynet/go-gpt2.cpp => /workspace/go-gpt2

replace github.com/donomii/go-rwkv.cpp => /workspace/go-rwkv

replace github.com/ggerganov/whisper.cpp => /workspace/whisper.cpp

replace github.com/go-skynet/go-bert.cpp => /workspace/go-bert

replace github.com/go-skynet/bloomz.cpp => /workspace/bloomz

replace github.com/mudler/go-stable-diffusion => /workspace/go-stable-diffusion

// replace github.com/deepmap/oapi-codegen v1.12.4 => github.com/dave-gray101/oapi-codegen v0.0.0

replace github.com/deepmap/oapi-codegen => github.com/dave-gray101/oapi-codegen v0.0.0-20230523054811-7942876e1d78

// replace github.com/deepmap/oapi-codegen/cmd/oapi-codegen => github.com/dave-gray101/oapi-codegen/cmd/oapi-codegen yaml_and_dep_filter
