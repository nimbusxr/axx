//go:build integration

package mlc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/models/core/internal/webllmrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// libs is the commit of MLC's prebuilt model libraries WebLLM 0.2.85 runs
// (its modelVersion's, v0_2_84).
const libs = "https://raw.githubusercontent.com/mlc-ai/binary-mlc-llm-libs/025bcaf3780fa8254f5e5efd3bfea0a5397248f4/web-llm-models/v0_2_84/base/"

// realModel is a real MLC model whose stubs WebLLM runs: its folder's files
// (but its weights' shards) at its repository's commit, and its model
// library, each pinned by its SHA-256.
type realModel struct {
	name, repo, commit string
	files              map[string]string
	lib, libSum        string
	// overrides are the config's, as WebLLM's prebuilt model's are (nil:
	// a context window of 4096).
	overrides map[string]any
	// seconds are how long its stubs took on CI's webllm runner (the job logs
	// each model's), which the models are shared out by.
	seconds int
}

// realModels are the real models: of each architecture WebLLM's prebuilt
// models are of, its quantizations.
var realModels = []realModel{
	{
		"SmolLM2 135M Instruct, 4-bit groups of halves' scales", "mlc-ai/SmolLM2-135M-Instruct-q4f16_1-MLC", "aefa3deb0223b157695b9ee84a54747bfec59fa1",
		map[string]string{
			"mlc-chat-config.json": "0b1497dfb91b6bb5a149e7e4994b9fa49eec3f7ebc2a8e4e7cd98aeacf0b66a4",
			"tensor-cache.json":    "71391bc4edd674127f2ded0db6a74c3e1720c1c17983d4231755de9e561696d6",
			"ndarray-cache.json":   "71391bc4edd674127f2ded0db6a74c3e1720c1c17983d4231755de9e561696d6",
			"tokenizer.json":       "9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
		},
		"SmolLM2-135M-Instruct-q4f16_1_cs1k-webgpu.wasm", "1883ad6c1b621ddcdb2c3ef9efe4cd97c16a6d2ef6ed683b528f8a031e3494ed", nil, 25,
	},
	{
		"SmolLM2 135M Instruct, 4-bit groups of float32s' scales", "mlc-ai/SmolLM2-135M-Instruct-q4f32_1-MLC", "142ac13dfa8857ff83132056ef4005a586ac4855",
		map[string]string{
			"mlc-chat-config.json":   "71cec9fb099c8326172b17d345e3f6c865d7958c5237f80dcad7c2e58ba5f830",
			"tensor-cache.json":      "392081e9b0865ba01701c70f824891284283db82afa27bfff7372754a3b26d7e",
			"ndarray-cache.json":     "392081e9b0865ba01701c70f824891284283db82afa27bfff7372754a3b26d7e",
			"ndarray-cache-b16.json": "6cd114522e9b63c9471ae8058f8983a78e0f527c0cfd4b7b437e5cae955e9190",
			"tokenizer.json":         "9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
		},
		"SmolLM2-135M-Instruct-q4f32_1_cs1k-webgpu.wasm", "e73dc9f4b165e4488b532dee6df1f07b64fdd7e22438ae953387526ca5f13e3b", nil, 30,
	},
	{
		"SmolLM2 135M Instruct, halves", "mlc-ai/SmolLM2-135M-Instruct-q0f16-MLC", "06fc409f2212adff29d93cbc742c1d6957302df5",
		map[string]string{
			"mlc-chat-config.json": "84fa8cda47c443589d05c7295339b0c58c15d964e7f2df53873366187789bb59",
			"tensor-cache.json":    "bb1307859901ebe833a1ad9f25b5a2babed8bbcfe8ea1d7e5b64db52e1c17149",
			"ndarray-cache.json":   "bb1307859901ebe833a1ad9f25b5a2babed8bbcfe8ea1d7e5b64db52e1c17149",
			"tokenizer.json":       "9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
		},
		"SmolLM2-135M-Instruct-q0f16_cs1k-webgpu.wasm", "124201e7d550944c578f67c813d74b861bee2b45c8478a694634599ba5364e64", nil, 45,
	},
	{
		"SmolLM2 135M Instruct, float32s (stored as bfloat16s)", "mlc-ai/SmolLM2-135M-Instruct-q0f32-MLC", "ba1ecf858a4dfc6ac3e668c388a7088bdfb3c091",
		map[string]string{
			"mlc-chat-config.json":   "0de93bedfc51d897971f208f1723e0d4b12c4804c08f024d4be976100df62806",
			"tensor-cache.json":      "d69211c94d41b078f5ed335d387d06c2ceed0e7fdc422db675e0f61eaa749daa",
			"ndarray-cache.json":     "d69211c94d41b078f5ed335d387d06c2ceed0e7fdc422db675e0f61eaa749daa",
			"ndarray-cache-b16.json": "1b04a0cf75f25746cd60ddd441bc320e8f49a965c67819b8c941b5a6d5cc63a5",
			"tokenizer.json":         "9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
		},
		"SmolLM2-135M-Instruct-q0f32_cs1k-webgpu.wasm", "18cfb3c0d7c265afffe936afb711295a02e6fe3cf7c68bc5b71dad89d84b1fec", nil, 25,
	},
	{
		"Qwen2.5 0.5B Instruct (Qwen 2's architecture: biases), 4-bit groups of halves' scales", "mlc-ai/Qwen2.5-0.5B-Instruct-q4f16_1-MLC", "32ff081fe7e4dfe4ffb167b94c66fdf11e02b8ad",
		map[string]string{
			"mlc-chat-config.json": "5439c03bdf4ee7cfe0f8a97a6588018b0144dc1280c71bbaea2853cdbab874b9",
			"tensor-cache.json":    "23a54b5e9b271204561901b513c5b359498733429ee219ccc907eb1cd0790e3f",
			"ndarray-cache.json":   "23a54b5e9b271204561901b513c5b359498733429ee219ccc907eb1cd0790e3f",
			"tokenizer.json":       "c0382117ea329cdf097041132f6d735924b697924d6f6fc3945713e96ce87539",
		},
		"Qwen2-0.5B-Instruct-q4f16_1_cs1k-webgpu.wasm", "611b584fd44af2789416395603965a6bc074f2127188af597f4dda016fbdab19", nil, 45,
	},
	{
		"Qwen2.5 0.5B Instruct, 4-bit groups of float32s' scales", "mlc-ai/Qwen2.5-0.5B-Instruct-q4f32_1-MLC", "8e6ec5f417c7d0e1414703ef619b8a24686e3ab0",
		map[string]string{
			"mlc-chat-config.json":   "6ac10b5805b22c82f1867d9be61530d03755e8741c2b7335f120ffa1c15ae8f1",
			"tensor-cache.json":      "bfb7adf1d3c1b8ea6606e6f126a126c575ce39381a4712ca65da992beb177e98",
			"ndarray-cache.json":     "bfb7adf1d3c1b8ea6606e6f126a126c575ce39381a4712ca65da992beb177e98",
			"ndarray-cache-b16.json": "9aac5251afa9fbb27596675965076436f724cd7c2d19d5f984855f033070d91c",
			"tokenizer.json":         "c0382117ea329cdf097041132f6d735924b697924d6f6fc3945713e96ce87539",
		},
		"Qwen2-0.5B-Instruct-q4f32_1_cs1k-webgpu.wasm", "13142a60890868c2c1626cf513cd848677d6658ade3b2278788dad81373efd7d", nil, 30,
	},
	{
		"Llama 3.2 1B Instruct (Llama 3's rotary scaling), 4-bit groups of halves' scales", "mlc-ai/Llama-3.2-1B-Instruct-q4f16_1-MLC", "2a37b0a5ecb622d51ddc2fac74de0b95872affd7",
		map[string]string{
			"mlc-chat-config.json": "0ec513b54b419ad471006430686bdcffaae7102b41f7b0246fbfcde2517acc7f",
			"tensor-cache.json":    "cf7c1825a9e0d7111bca7f65d8d9de7789f9121579e9500150469543a2975c17",
			"ndarray-cache.json":   "cf7c1825a9e0d7111bca7f65d8d9de7789f9121579e9500150469543a2975c17",
			"tokenizer.json":       "79e3e522635f3171300913bb421464a87de6222182a0570b9b2ccba2a964b2b4",
		},
		"Llama-3.2-1B-Instruct-q4f16_1_cs1k-webgpu.wasm", "2aa9b5f0c8de532f6cbf6bb7b863aaa02645bf6fff856083b202f968712d3f92", nil, 75,
	},
	{
		"Llama 3.2 1B Instruct, 4-bit groups of float32s' scales", "mlc-ai/Llama-3.2-1B-Instruct-q4f32_1-MLC", "a949835e0f9a65335bd834db02a44cf8fae34020",
		map[string]string{
			"mlc-chat-config.json":   "046561e5a48d0c926b1fca563720c53487b59149fe958a189a8ee6c08e0eef34",
			"tensor-cache.json":      "26795cef96623f47b69fb8c1aaaf0a8aa7c9381a446f35e2de6ca104271f44db",
			"ndarray-cache.json":     "26795cef96623f47b69fb8c1aaaf0a8aa7c9381a446f35e2de6ca104271f44db",
			"ndarray-cache-b16.json": "0a986f3e5198f6067cacba0ed91f5622cc1fb1fafea4f1b531c679ad4aa091c6",
			"tokenizer.json":         "79e3e522635f3171300913bb421464a87de6222182a0570b9b2ccba2a964b2b4",
		},
		"Llama-3.2-1B-Instruct-q4f32_1_cs1k-webgpu.wasm", "a1407404cb85b7dbc6db4bd77569291f02dab50aaeb1dc595b17595c2e204c66", nil, 60,
	},
	{
		"TinyLlama 1.1B Chat (an output of its own: lm_head), 4-bit groups of halves' scales", "mlc-ai/TinyLlama-1.1B-Chat-v1.0-q4f16_1-MLC", "aa4e41dd1861bb2469f9ddfb8fa3054b857a00ed",
		map[string]string{
			"mlc-chat-config.json": "76ef6953b65e5172f691a036cdf627c33d864b6f0b7ab458d40ef846c9cdffc6",
			"tensor-cache.json":    "1db8f75d6a22bc2694931510393ff9104dbca689825d4a5319d2b0e50f5ff21a",
			"ndarray-cache.json":   "1db8f75d6a22bc2694931510393ff9104dbca689825d4a5319d2b0e50f5ff21a",
			"tokenizer.json":       "bcd04f0eadf90287bd26e1a183ac487d8a141b09b06aecb7725bbdd343640f2e",
		},
		"TinyLlama-1.1B-Chat-v1.0-q4f16_1_cs1k-webgpu.wasm", "843ee745703f41e716740ceecdbf5cdb27e7a28df35b68aad909c7df6b1573ea",
		map[string]any{"context_window_size": 2048},
		50,
	},
	{
		"TinyLlama 1.1B Chat, 4-bit groups of float32s' scales", "mlc-ai/TinyLlama-1.1B-Chat-v1.0-q4f32_1-MLC", "c414459233920b88c45096e8c6b274b0f0e607c5",
		map[string]string{
			"mlc-chat-config.json":   "ce4d138682fe779fbda2c4a9222f1969350dd45416d615e6931768e97c06da39",
			"tensor-cache.json":      "73ccda45674406b75dedc4b386818ba97926324beed8420319cbf830dc9113f4",
			"ndarray-cache.json":     "73ccda45674406b75dedc4b386818ba97926324beed8420319cbf830dc9113f4",
			"ndarray-cache-b16.json": "e43e30772da74871478ea59668848260eb14b4ffecbf1e89999b6a8852fc3f68",
			"tokenizer.json":         "bcd04f0eadf90287bd26e1a183ac487d8a141b09b06aecb7725bbdd343640f2e",
		},
		"TinyLlama-1.1B-Chat-v1.0-q4f32_1_cs1k-webgpu.wasm", "1ca145463bed8d1cbaeb1838f8d75946f3ed109e2d5df58b841a6258b2a49af5",
		map[string]any{"context_window_size": 2048},
		40,
	},
	{
		"Qwen3 0.6B (Qwen 3's heads' norms), 4-bit groups of halves' scales", "mlc-ai/Qwen3-0.6B-q4f16_1-MLC", "8c14ce481d4c692769976ad52afea453a102df19",
		map[string]string{
			"mlc-chat-config.json": "190a51c56b9a07a8d8721266f8e459bcd1b1689bcdff294795cd2bffa203ff2d",
			"tensor-cache.json":    "f4647d617dc869d91fbd7851c65af0a2cf7dd615884ebe3b110013801b65d8c7",
			"ndarray-cache.json":   "f4647d617dc869d91fbd7851c65af0a2cf7dd615884ebe3b110013801b65d8c7",
			"tokenizer.json":       "aeb13307a71acd8fe81861d94ad54ab689df773318809eed3cbe794b4492dae4",
		},
		"Qwen3-0.6B-q4f16_1_cs1k-webgpu.wasm", "4db800b24119204e1a0386e8a12e084d5012aa60f77c5bffad362f20498df912", nil, 60,
	},
	{
		"Qwen3 0.6B, 4-bit groups of float32s' scales", "mlc-ai/Qwen3-0.6B-q4f32_1-MLC", "9b4f0b05b08c692ea86fe151ea878406ad22f428",
		map[string]string{
			"mlc-chat-config.json":   "e52fcd32f38eaa9d8886fd3e9e0f0f13334226784a9fd8f4db2acffff01653cf",
			"tensor-cache.json":      "625c67c44d4fcc7a2c3915257634a5866b1c2e45911be05ecedcfee6e04b22ea",
			"ndarray-cache.json":     "625c67c44d4fcc7a2c3915257634a5866b1c2e45911be05ecedcfee6e04b22ea",
			"ndarray-cache-b16.json": "bfb35ec04c6caee3fa6bb9d3b4efa3223b456105b9135035d65bb6c3dc41f509",
			"tokenizer.json":         "aeb13307a71acd8fe81861d94ad54ab689df773318809eed3cbe794b4492dae4",
		},
		"Qwen3-0.6B-q4f32_1_cs1k-webgpu.wasm", "361f1310ae616863bccbc638f075f7484816b45eb30ffecf08539272a1b2ae1e", nil, 40,
	},
	{
		"Phi-3.5 mini Instruct (Phi-3's architecture, longrope's angles), 4-bit groups of halves' scales", "mlc-ai/Phi-3.5-mini-instruct-q4f16_1-MLC", "206a7da25b5efbef7b50502993367fcb05183a1d",
		map[string]string{
			"mlc-chat-config.json": "9337233c264de13055be2cae4be2e34f63870648c1dbba29f7a0708d8bc9c60b",
			"tensor-cache.json":    "054232d3a793dbd124ec4fafd7d0e89a0cacd2cacf8c9ebacb7a4de7bab151ba",
			"ndarray-cache.json":   "054232d3a793dbd124ec4fafd7d0e89a0cacd2cacf8c9ebacb7a4de7bab151ba",
			"tokenizer.json":       "9e2ae3d66819f163cdcfedba5078f3a3af8118c2712491ed60912f77886bda6f",
		},
		"Phi-3.5-mini-instruct-q4f16_1_cs1k-webgpu.wasm", "0342d0b660eb0d0415d37e4689c77bdab6dd2ca89cf5a44a0623d4c962e67c77", nil, 135,
	},
	{
		"Phi-4 mini Instruct (heads' dimensions that do not turn; a vocabulary of 200,064 tokens), 4-bit groups of halves' scales", "mlc-ai/Phi-4-mini-instruct-q4f16_1-MLC", "c2ecf2bf3a81d201ccdbfb9019d67fb8f0dede52",
		map[string]string{
			"mlc-chat-config.json": "f99764504eda475edc425d6c8769005eedd35c7ae26e402c79edf722c9a239fc",
			"tensor-cache.json":    "78da9cdf911e5ec34cc6733834bdcb9532bbaf2e89bb3ce79e3a4e944b8bae30",
			"ndarray-cache.json":   "78da9cdf911e5ec34cc6733834bdcb9532bbaf2e89bb3ce79e3a4e944b8bae30",
			"tokenizer.json":       "382cc235b56c725945e149cc25f191da667c836655efd0857b004320e90e91ea",
		},
		"Phi-4-mini-instruct-q4f16_1_cs1k-webgpu.wasm", "5d24b86a0bab130f0ab04bd7a208294423ff69c8f35c3254c873bf17fb7020d6", nil, 130,
	},
	{
		"Mistral 7B Instruct v0.3 (Mistral's architecture; its tokenizer's Metaspace), 4-bit groups of halves' scales", "mlc-ai/Mistral-7B-Instruct-v0.3-q4f16_1-MLC", "e06f3992dc08bd7196602031577a550cbfdefc54",
		map[string]string{
			"mlc-chat-config.json": "bde256b655e558759791194af35780639c07ba674597ca0e994d40c87f7eff83",
			"tensor-cache.json":    "9ba584a1de465aef5cb259d2ae9d8add4b85b1018a83a528d858503eac9a1576",
			"ndarray-cache.json":   "9ba584a1de465aef5cb259d2ae9d8add4b85b1018a83a528d858503eac9a1576",
			"tokenizer.json":       "e553af6fff7d7ad76e830608b218c5c0b0822998d5a1a96099a74cd3c1cb1a49",
		},
		"Mistral-7B-Instruct-v0.3-q4f16_1_cs1k-webgpu.wasm", "e2b2454cfc5958ef879a2839cecacfafb7af8947b7927e2ea72bc24dfc46db7d",
		map[string]any{"context_window_size": 4096, "sliding_window_size": -1},
		325,
	},
	{
		"Gemma 2B (Gemma's embeddings × √E, GELU, a key-value head of 8 queries), 4-bit groups of halves' scales", "mlc-ai/gemma-2b-it-q4f16_1-MLC", "a2d2c454207b5a4d3b7d88311f06f89d8a6018d5",
		map[string]string{
			"mlc-chat-config.json": "7e0207a9397d16e6ae6534de63a7b503f53d32239f6b66cf6884ead6a26c084a",
			"tensor-cache.json":    "d251cfc8d282176297d9d25fa20dbf457e0b2e5cc1693225cbee808b2ade2789",
			"ndarray-cache.json":   "d251cfc8d282176297d9d25fa20dbf457e0b2e5cc1693225cbee808b2ade2789",
			"tokenizer.json":       "05e97791a5e007260de1db7e1692e53150e08cea481e2bf25435553380c147ee",
		},
		"gemma-2b-it-q4f16_1_cs1k-webgpu.wasm", "935c6de0976baa1b32abfb9cde8a7940bb9d7a560d9ca3fbd4886dee5e61130d", nil, 85,
	},
	{
		"Gemma 2B, 4-bit groups of float32s' scales", "mlc-ai/gemma-2b-it-q4f32_1-MLC", "2ec7861816a552592c2ca0849924cb26368ebb9a",
		map[string]string{
			"mlc-chat-config.json":   "13d05b693ce4b885ee025d2d53847f4b1fe91bfcdeda1c6c46201c4254dbaa61",
			"tensor-cache.json":      "4379366611e1d888eb650fbd96d1afb1461b7cb862473ea5ca0382b1daa2d061",
			"ndarray-cache.json":     "4379366611e1d888eb650fbd96d1afb1461b7cb862473ea5ca0382b1daa2d061",
			"ndarray-cache-b16.json": "3b4c6c39867fdde7d030314e45a9e80a968aa63d450fd58647ab1b200abcbe9f",
			"tokenizer.json":         "05e97791a5e007260de1db7e1692e53150e08cea481e2bf25435553380c147ee",
		},
		"gemma-2b-it-q4f32_1_cs1k-webgpu.wasm", "392d9041aa122e56623822df7662f45d1e64a5a49f169c1042c9fa1911a82332", nil, 155,
	},
	{
		"Gemma 2 2B (its sublayers' outputs normalized, its logits capped), 4-bit groups of halves' scales", "mlc-ai/gemma-2-2b-it-q4f16_1-MLC", "de9cc76f0d4b3a49a0f718df424944054bf1eec1",
		map[string]string{
			"mlc-chat-config.json": "4d2954ec2459fe7d3cf7de1edbba7ec24df2498ce9769482e49772b781165a67",
			"tensor-cache.json":    "9674d4b0968b9ad288cd777a683190c4594706bd16dad99f70ba5d8bd911a3a3",
			"ndarray-cache.json":   "9674d4b0968b9ad288cd777a683190c4594706bd16dad99f70ba5d8bd911a3a3",
			"tokenizer.json":       "3f289bc05132635a8bc7aca7aa21255efd5e18f3710f43e3cdb96bcd41be4922",
		},
		"gemma-2-2b-it-q4f16_1_cs1k-webgpu.wasm", "a1e849029df21b68674bbd811f5b1d6d58cc8c22689bb580dc19faf53d7be364", nil, 115,
	},
	{
		"Gemma 2 2B, 4-bit groups of float32s' scales", "mlc-ai/gemma-2-2b-it-q4f32_1-MLC", "5389274865dee69fc1c0f44af37990d933fb4c47",
		map[string]string{
			"mlc-chat-config.json":   "7905a50510cd4de2c70e4971b29ac7b94b5d06e94a1a2a767a6bdb1301e86cf4",
			"tensor-cache.json":      "cff3028de792af8e85d9a1be02299624275069b015e9c87bb37a2f8cb0a70609",
			"ndarray-cache.json":     "cff3028de792af8e85d9a1be02299624275069b015e9c87bb37a2f8cb0a70609",
			"ndarray-cache-b16.json": "00a9291839761c560dcc413c71b2ce0d8df692bdcc62c8998c60b04b3546b86f",
			"tokenizer.json":         "3f289bc05132635a8bc7aca7aa21255efd5e18f3710f43e3cdb96bcd41be4922",
		},
		"gemma-2-2b-it-q4f32_1_cs1k-webgpu.wasm", "1f9d0ca4a8b0edf87c2f3326948baa61fdf5f05223bb322f5e26caa8240d16e7", nil, 110,
	},
	{
		// WebLLM takes a context window or a sliding window, not both:
		// its prebuilt Gemma 3 gives its context window alone, of a
		// config of both, which it does not load; an app gives no sliding
		// window too, as WebLLM's prebuilt Mistral does.
		"Gemma 3 1B (Gemma 3's own projections and heads' norms; a key-value head of 4 queries)", "mlc-ai/gemma3-1b-it-q4f16_1-MLC", "0f6103ed635931ff91a95db543365f0a77cbfb7d",
		map[string]string{
			"mlc-chat-config.json": "02fb5180f7b400c65aa95d6375730b571d3bdc8b8ff58901084cb9c8da18b7a2",
			"tensor-cache.json":    "13df387cc76f314fb518414e6ce0f8dd969e247abd69e075e649659c23b89fa0",
			"tokenizer.json":       "4667f2089529e8e7657cfb6d1c19910ae71ff5f28aa7ab2ff2763330affad795",
		},
		"gemma3-1b-it-q4f16_1_cs1k-webgpu.wasm", "c608d6eb62de3b7dd7444d3d48088f073e3b68a719dfe77e111efac70dd2e165",
		map[string]any{"context_window_size": 4096, "sliding_window_size": -1},
		75,
	},
	{
		"OLMo 2 1B (no input norms; its queries' and keys' norms of the whole projection), 4-bit groups of halves' scales", "mlc-ai/OLMo-2-0425-1B-Instruct-q4f16_1-MLC", "a3fef630301a7e9a4dcbbdd3a76b0d36b20dc85e",
		map[string]string{
			"mlc-chat-config.json": "e0c8ee2b06d99c305f09398821be187e250bed4f2aab5b894aa88bb235829494",
			"tensor-cache.json":    "0112e7d03d1e1f9766e047056a1ba9a85ad7f5f56add6ab517982f8a868030e7",
			"tokenizer.json":       "73fd5254624f39a88e3faac6a8e11300fc3c735ed37880d4f4f08db898eaecca",
		},
		"OLMo-2-0425-1B-Instruct-q4f16_1_cs1k-webgpu.wasm", "b581aa534e304c1d15956acbca9e4f8f53786ba51b827b2b14819a82e6852194", nil, 85,
	},
	{
		"OLMo 2 1B, 4-bit groups of float32s' scales", "mlc-ai/OLMo-2-0425-1B-Instruct-q4f32_1-MLC", "cbcbcfa8e266f1b19adc2c4d50a19323eb58886c",
		map[string]string{
			"mlc-chat-config.json": "364e54904f95a1555538394df816d0b668005ac34bd0dcc5033c80a44aad3b3b",
			"tensor-cache.json":    "6c25dba6b2c4e717c9cf426aa9046f9bcbc7c8b2e25443b3ebcd10f586ee6d9c",
			"tokenizer.json":       "73fd5254624f39a88e3faac6a8e11300fc3c735ed37880d4f4f08db898eaecca",
		},
		"OLMo-2-0425-1B-Instruct-q4f32_1_cs1k-webgpu.wasm", "0717a79936dddc206f9484e818be664673bb11a9b969d994e7c60eae70e4d04c", nil, 70,
	},
	{
		"StableLM 2 Zephyr 1.6B (LayerNorm's, of biases; a quarter of its heads' dimensions turning), 4-bit groups of halves' scales", "mlc-ai/stablelm-2-zephyr-1_6b-q4f16_1-MLC", "9457f520b3ec10c325f1b37bdf8e4a722329edb9",
		map[string]string{
			"mlc-chat-config.json": "47fd6d4861708ca7b1a801c0260a1ababe41737688a743fb7278016a938670b3",
			"tensor-cache.json":    "ee22e4d1dea12c217cf2bef63449f1e6af8f4b3b519cea8f63311f08e9a3904a",
			"ndarray-cache.json":   "ee22e4d1dea12c217cf2bef63449f1e6af8f4b3b519cea8f63311f08e9a3904a",
			"tokenizer.json":       "4ce5f0768574d7224669ee2192ce9b45394b64df3d6e7ea7e36f8ee2d3a9d6d8",
		},
		"stablelm-2-zephyr-1_6b-q4f16_1_cs1k-webgpu.wasm", "404e18551f880b7935c1c57561898eb686e00bf89c3cb543acfefd4bab9a373d", nil, 55,
	},
	{
		"StableLM 2 Zephyr 1.6B, 4-bit groups of float32s' scales", "mlc-ai/stablelm-2-zephyr-1_6b-q4f32_1-MLC", "037e1b5c3687743859a874af1796121e709ee7d8",
		map[string]string{
			"mlc-chat-config.json":   "c998affa256cdbfdfb274c7325148f219a86fcae0da5998bd7a06dbbcb608904",
			"tensor-cache.json":      "caa91d3a6a1c9811d540194d57634007f91b59b6dc2d52c4579d41f42d5cfb0b",
			"ndarray-cache.json":     "caa91d3a6a1c9811d540194d57634007f91b59b6dc2d52c4579d41f42d5cfb0b",
			"ndarray-cache-b16.json": "7da7bf90f6f959972d742c3f69e6db43ad2237286aa71b05f553f31309475add",
			"tokenizer.json":         "4ce5f0768574d7224669ee2192ce9b45394b64df3d6e7ea7e36f8ee2d3a9d6d8",
		},
		"stablelm-2-zephyr-1_6b-q4f32_1_cs1k-webgpu.wasm", "3d70e46ca3b3c79199eb13a3d6230bd3a372e97a95cc8839b9f05cd80a0c4b9d", nil, 60,
	},
	{
		"RedPajama INCITE Chat 3B (GPT-NeoX's architecture: an MLP of no gate, biases), 4-bit groups of halves' scales", "mlc-ai/RedPajama-INCITE-Chat-3B-v1-q4f16_1-MLC", "3400b5d0cd1a7721031c6507ae6d9d6ba3b6e1bd",
		map[string]string{
			"mlc-chat-config.json": "6f3d85d91a67de90e7bac6b77f0825a8ccd2cad6f100a0f79864284067b75e68",
			"tensor-cache.json":    "3d3cdb9567b4a67042064e2b9c67e76878da552f347f085b42342596c2a44fdf",
			"ndarray-cache.json":   "3d3cdb9567b4a67042064e2b9c67e76878da552f347f085b42342596c2a44fdf",
			"tokenizer.json":       "3cf430678137c8491ca82fb7092ee49e44ad38857fffe1e4a4a5ed860139a5b8",
		},
		"RedPajama-INCITE-Chat-3B-v1-q4f16_1_cs1k-webgpu.wasm", "bfe8fc46926481e547b3b47bbbb0ecf20334ed6148c77e5a30b6cdb6ad20ada3",
		map[string]any{"context_window_size": 2048},
		95,
	},
	{
		"RedPajama INCITE Chat 3B, 4-bit groups of float32s' scales", "mlc-ai/RedPajama-INCITE-Chat-3B-v1-q4f32_1-MLC", "d36f6e7492de32249394ce12111a5f28f31681b1",
		map[string]string{
			"mlc-chat-config.json": "ef5e2ca775e44b83491f4eacac2f37fbd9f5d14aa08b13e57fff253e7b9ec6bb",
			"tensor-cache.json":    "e0b62cc60ba8b6cd4494dde3fd3135e6a7824410c741ef92b916447795501def",
			"ndarray-cache.json":   "e0b62cc60ba8b6cd4494dde3fd3135e6a7824410c741ef92b916447795501def",
			"tokenizer.json":       "3cf430678137c8491ca82fb7092ee49e44ad38857fffe1e4a4a5ed860139a5b8",
		},
		"RedPajama-INCITE-Chat-3B-v1-q4f32_1_cs1k-webgpu.wasm", "d7ea748ddd12dc4f6b9d948590acae3d08b56e83f7127f9bbbdfaab50cd8a2fa",
		map[string]any{"context_window_size": 2048},
		140,
	},
	{
		"Phi-2 (its attention and MLP in parallel, of one LayerNorm; a head of its own, of a LayerNorm and a bias), 4-bit groups of halves' scales", "mlc-ai/phi-2-q4f16_1-MLC", "c0e829d9a02ab956a96380c668e48c2adc7ab28a",
		map[string]string{
			"mlc-chat-config.json": "33207a9ec4ad0eb8235409ac9cd1da1c7445324fd4842d923c71e9d09bf6d5eb",
			"tensor-cache.json":    "57a8e020e1d9303ccc37b3468eea42d18899f52870cbe1915a7c5cd36429de4b",
			"ndarray-cache.json":   "57a8e020e1d9303ccc37b3468eea42d18899f52870cbe1915a7c5cd36429de4b",
			"tokenizer.json":       "337da36be7a71a6e88aa9148967a7bc8736f4b47c7de8e19ba92b89e80734cfc",
		},
		"phi-2-q4f16_1_cs1k-webgpu.wasm", "b89abe35524310f85db3fc533144ff2bf8e8a840adb0d4ff834361bdf4e4b187",
		map[string]any{"context_window_size": 2048},
		120,
	},
	{
		"Phi-2, 4-bit groups of float32s' scales", "mlc-ai/phi-2-q4f32_1-MLC", "1f90007d454e3bfea05f45f4bd21024b63cf9a2b",
		map[string]string{
			"mlc-chat-config.json":   "cec2b82121fb820e4711461f81085cb5c0c2195706e572e86db23ebe4016f51e",
			"tensor-cache.json":      "c6f3f3801e7efc6dafb0e48f40ad1bbb7a856c5de9779298cf2a382008bd0836",
			"ndarray-cache.json":     "c6f3f3801e7efc6dafb0e48f40ad1bbb7a856c5de9779298cf2a382008bd0836",
			"ndarray-cache-b16.json": "9102c5ede08048a1f2a80dcda76a128ab189a92d1f195b6cd7d4b46cec3a0044",
			"tokenizer.json":         "337da36be7a71a6e88aa9148967a7bc8736f4b47c7de8e19ba92b89e80734cfc",
		},
		"phi-2-q4f32_1_cs1k-webgpu.wasm", "ca5b18c376b53067a317dc14f13103bce26c256ec2499d96b30fadd1586b122e",
		map[string]any{"context_window_size": 2048},
		90,
	},
	{
		"Phi-1.5, 4-bit groups of halves' scales", "mlc-ai/phi-1_5-q4f16_1-MLC", "48ccb37900769345f634cf56f1d7afafbc27d2ec",
		map[string]string{
			"mlc-chat-config.json": "c47abf732efa984815cfb0cb7325c28bafdd424c08a4b733b296c036b48d2062",
			"tensor-cache.json":    "e6a365ae474621553d0a6f263b9c086fb97d18e6a5b77c3edd82c8c51b5129c1",
			"ndarray-cache.json":   "e6a365ae474621553d0a6f263b9c086fb97d18e6a5b77c3edd82c8c51b5129c1",
			"tokenizer.json":       "337da36be7a71a6e88aa9148967a7bc8736f4b47c7de8e19ba92b89e80734cfc",
		},
		"phi-1_5-q4f16_1_cs1k-webgpu.wasm", "853591c72bf9079f8705b16d2ecf7eb50ee6a9f80eeacb60ec6c50ecab44ee8d",
		map[string]any{"context_window_size": 2048},
		65,
	},
	{
		"Qwen3.5 0.8B (full attention every fourth layer, of its outputs' gates; the others' linear attention's of no weights), 4-bit groups of halves' scales", "mlc-ai/Qwen3.5-0.8B-q4f16_1-MLC", "0ec138972555613c1d7812a821778ad0398c8790",
		map[string]string{
			"mlc-chat-config.json": "1ea93c2b8b396a3754516d0c4b6f4b6881c2930ca2cc6cb2cf00e511e6be86a9",
			"tensor-cache.json":    "bab74fa529512ca7641f6a4aea0baeab26742d28886e79aac11b09fa8738c0df",
			"tokenizer.json":       "5f9e4d4901a92b997e463c1f46055088b6cca5ca61a6522d1b9f64c4bb81cb42",
		},
		"Qwen3.5-0.8B-q4f16_1_cs1k-webgpu.wasm", "3dd8cff049bf4599bfbb505880aea11ba95f85f3924b7e171f967d1f1348ae29", nil, 45,
	},
	{
		"Qwen3.5 0.8B, 4-bit groups of float32s' scales", "mlc-ai/Qwen3.5-0.8B-q4f32_1-MLC", "4ed017713f04c91ffe55a9dca8c7ac853c41314d",
		map[string]string{
			"mlc-chat-config.json": "f5dfccfadabb998a9ae7d369d51e5611bddae5f4e01a2bf7cb245673ad589470",
			"tensor-cache.json":    "d6588a3221565274baf7423297aa32105de23ec6ac211469a1340bfac73b807d",
			"tokenizer.json":       "5f9e4d4901a92b997e463c1f46055088b6cca5ca61a6522d1b9f64c4bb81cb42",
		},
		"Qwen3.5-0.8B-q4f32_1_cs1k-webgpu.wasm", "48232ec3bc756521463ca9bc49ff036d82ea9c7930d2a41ef6d9bfb20e5c684d", nil, 70,
	},
	{
		// Its chats of text: WebLLM's library of it prefills chunks of 2048
		// embeddings at most, and an image is of 2509 (16 crops' and a
		// global view's), which WebLLM refuses.
		"Phi-3.5 vision Instruct (Phi-3's text model; its vision encoder of no weights, an image's tokens of no code), 4-bit groups of halves' scales", "mlc-ai/Phi-3.5-vision-instruct-q4f16_1-MLC", "b40f2bfae88f59715ae109cc9d82d13ae8b2ea52",
		map[string]string{
			"mlc-chat-config.json": "5ea339d9304897dec7da1a456fa1a9e989fdbe58bda0b8fdbe7f180b0ec34ad6",
			"tensor-cache.json":    "779096b2ccaeae107afd7970cfb40c3abcc60a89fc62f471274f4d6859904137",
			"ndarray-cache.json":   "779096b2ccaeae107afd7970cfb40c3abcc60a89fc62f471274f4d6859904137",
			"tokenizer.json":       "493f8ad437be806d5ccdc57e91479bdbb0c656448c1b89ff70f7653530d78518",
		},
		"Phi-3.5-vision-instruct-q4f16_1_cs2k-webgpu.wasm", "6af42cc69564ed5477de104ddeb407f4492e426cf3005b005f01b338dfa6c15c", nil, 185,
	},
}

// WebLLM answers a stub's chats as its mappings say, with the real model's
// library on the machine's GPU, at the context window of WebLLM's prebuilt
// models: greedily and sampled at the model's temperature, in
// conversations that keep their turns. A library of halves
// needs a GPU of halves in its shaders; a machine of no GPU (a Linux
// runner's, whose WebGPU is SwiftShader's, on its processor, too slow for
// these models) runs none.
func TestWebLLMAnswersAsTheStubSays(t *testing.T) { conformWebLLM(t, realModels) }

func conformWebLLM(t *testing.T, models []realModel) {
	ctx := context.Background()
	rt, err := webllmrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var conversations [][]string
	var want [][]string
	for _, c := range modelstest.LanguageChats {
		conversations = append(conversations, []string{c.Prompt})
		want = append(want, []string{c.Want})
	}
	conversations = append(conversations, []string{"Ring the bell twice.", "Also: the box is fragile.", "Thanks!"})
	fragile := `{"handling":"fragile","summary":"Handle with care."}`
	want = append(want, []string{"No special instructions.", fragile, fragile})

	// One model at a time: a stub is of the real model's size (GBs), and the
	// browser's cache keeps a copy of it while it runs.
	// AXX_WEBLLM_SHARD=i/n runs the models of share i of n alone: CI's
	// runners take a share each, of about the same time.
	shard, shards := 0, 1
	if v := os.Getenv("AXX_WEBLLM_SHARD"); v != "" {
		if _, err := fmt.Sscanf(v, "%d/%d", &shard, &shards); err != nil || shards < 1 || shard < 0 || shard >= shards {
			t.Fatalf("AXX_WEBLLM_SHARD=%q is not i/n", v)
		}
	}
	seconds := make([]int, len(models))
	for i, m := range models {
		seconds[i] = m.seconds
	}
	ran, why := 0, ""
	for _, i := range shares(seconds, shards)[shard] {
		m := models[i]
		start := time.Now()
		real := t.TempDir()
		for name, sum := range m.files {
			b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+m.repo+"/resolve/"+m.commit+"/"+name, sum))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(real, name), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		lib := modelstest.Fetch(t, libs+m.lib, m.libSum)
		dir := stubOf(t, real, modelstest.LanguageMappings)
		var jobs []webllmrun.Job
		for _, temperature := range []float64{0, 1} {
			overrides := m.overrides
			if overrides == nil {
				overrides = map[string]any{"context_window_size": 4096}
			}
			jobs = append(jobs, webllmrun.Job{
				Model: dir, Lib: lib, F16: strings.Contains(m.lib, "f16"), Overrides: overrides, System: modelstest.LanguageSystem,
				Conversations: conversations, Temperature: temperature,
			})
		}
		results, err := rt.Run(ctx, jobs)
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		for j, job := range jobs {
			if results[j].Unavailable != "" {
				why = results[j].Unavailable
				t.Logf("%s: not run: %s", m.name, why)
				continue
			}
			ran++
			for i, conv := range conversations {
				for k, w := range want[i] {
					if got := results[j].Answers[i][k]; got != w {
						t.Errorf("%s, at temperature %v: %q: answered %q, want %q", m.name, job.Temperature, conv[k], got, w)
					}
				}
			}
		}
		t.Logf("%s: %s (about %ds)", m.name, time.Since(start).Round(time.Second), m.seconds)
	}
	if ran == 0 {
		t.Skipf("no library ran: %s", why)
	}
}
