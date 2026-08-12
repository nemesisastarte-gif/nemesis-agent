package consts

const (
	ModelApiKeyPrefix = "public:model:"
)

func PublicModelKey(key string) string {
	return ModelApiKeyPrefix + key
}

type ModelProvider string

const (
	ModelProviderSiliconFlow ModelProvider = "SiliconFlow"
	ModelProviderOpenAI      ModelProvider = "OpenAI"
	ModelProviderOllama      ModelProvider = "Ollama"
	ModelProviderDeepSeek    ModelProvider = "DeepSeek"
	ModelProviderMoonshot    ModelProvider = "Moonshot"
	ModelProviderAzureOpenAI ModelProvider = "AzureOpenAI"
	ModelProviderBaiZhiCloud ModelProvider = "BaiZhiCloud"
	ModelProviderHunyuan     ModelProvider = "Hunyuan"
	ModelProviderBaiLian     ModelProvider = "BaiLian"
	ModelProviderVolcengine  ModelProvider = "Volcengine"
	ModelProviderGoogle      ModelProvider = "Gemini"
	// ModelProviderNVIDIA NVIDIA NIM（OpenAI 兼容端点）。
	ModelProviderNVIDIA ModelProvider = "NVIDIA"
	// ModelProviderFireworks Fireworks AI（OpenAI 兼容端点）。
	ModelProviderFireworks ModelProvider = "Fireworks"
	// ModelProviderCohere Cohere（OpenAI 兼容端点 /compatibility/v1）。
	ModelProviderCohere ModelProvider = "Cohere"
	// ModelProviderCustom 自定义 provider（OpenAI 兼容格式，任意 base URL）。
	ModelProviderCustom ModelProvider = "Custom"
)

type InterfaceType string

const (
	InterfaceTypeOpenAIChat     InterfaceType = "openai_chat"
	InterfaceTypeOpenAIResponse InterfaceType = "openai_responses"
	InterfaceTypeAnthropic      InterfaceType = "anthropic"
)
