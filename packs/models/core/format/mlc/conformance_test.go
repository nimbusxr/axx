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

// libs is the commit of MLC's prebuilt model libraries WebLLM 0.2.88 runs
// (its modelVersion's, v0_2_87).
const libs = "https://raw.githubusercontent.com/mlc-ai/binary-mlc-llm-libs/146ed9cd4b4062e967b017fd2291f7106b50599f/web-llm-models/v0_2_87/base/"

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
		"SmolLM2-135M-Instruct-q4f16_1_cs1k-webgpu.wasm", "a9beabbec53f17d5068faf386a99e72e2e0d9c49767a5555b10366ce35aba335", nil, 25,
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
		"SmolLM2-135M-Instruct-q4f32_1_cs1k-webgpu.wasm", "bf87efeadf5ed3abca57a2503a44221f0935ec589532186c5b328512eeb0d697", nil, 30,
	},
	{
		"SmolLM2 135M Instruct, halves", "mlc-ai/SmolLM2-135M-Instruct-q0f16-MLC", "06fc409f2212adff29d93cbc742c1d6957302df5",
		map[string]string{
			"mlc-chat-config.json": "84fa8cda47c443589d05c7295339b0c58c15d964e7f2df53873366187789bb59",
			"tensor-cache.json":    "bb1307859901ebe833a1ad9f25b5a2babed8bbcfe8ea1d7e5b64db52e1c17149",
			"ndarray-cache.json":   "bb1307859901ebe833a1ad9f25b5a2babed8bbcfe8ea1d7e5b64db52e1c17149",
			"tokenizer.json":       "9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
		},
		"SmolLM2-135M-Instruct-q0f16_cs1k-webgpu.wasm", "853eecabfe29da72742904c345581de8afbdb2e0ebc671daa79199d8f550a3fd", nil, 45,
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
		"SmolLM2-135M-Instruct-q0f32_cs1k-webgpu.wasm", "cd367573bec87cbef4e109f0c14e8eed27b54cdbf9b341cb1b479b507a7e61b1", nil, 25,
	},
	{
		"Qwen2.5 0.5B Instruct (Qwen 2's architecture: biases), 4-bit groups of halves' scales", "mlc-ai/Qwen2.5-0.5B-Instruct-q4f16_1-MLC", "32ff081fe7e4dfe4ffb167b94c66fdf11e02b8ad",
		map[string]string{
			"mlc-chat-config.json": "5439c03bdf4ee7cfe0f8a97a6588018b0144dc1280c71bbaea2853cdbab874b9",
			"tensor-cache.json":    "23a54b5e9b271204561901b513c5b359498733429ee219ccc907eb1cd0790e3f",
			"ndarray-cache.json":   "23a54b5e9b271204561901b513c5b359498733429ee219ccc907eb1cd0790e3f",
			"tokenizer.json":       "c0382117ea329cdf097041132f6d735924b697924d6f6fc3945713e96ce87539",
		},
		"Qwen2-0.5B-Instruct-q4f16_1_cs1k-webgpu.wasm", "2a203aa9280044fd4b6d279c2c8bf3ef11e9e450435b973ef494e5d461e4f2dd", nil, 45,
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
		"Qwen2-0.5B-Instruct-q4f32_1_cs1k-webgpu.wasm", "6ba8b8f85be2e86f7ca16454d199bfc83e672ec010f4f6e265240c22505f5829", nil, 30,
	},
	{
		"Llama 3.2 1B Instruct (Llama 3's rotary scaling), 4-bit groups of halves' scales", "mlc-ai/Llama-3.2-1B-Instruct-q4f16_1-MLC", "2a37b0a5ecb622d51ddc2fac74de0b95872affd7",
		map[string]string{
			"mlc-chat-config.json": "0ec513b54b419ad471006430686bdcffaae7102b41f7b0246fbfcde2517acc7f",
			"tensor-cache.json":    "cf7c1825a9e0d7111bca7f65d8d9de7789f9121579e9500150469543a2975c17",
			"ndarray-cache.json":   "cf7c1825a9e0d7111bca7f65d8d9de7789f9121579e9500150469543a2975c17",
			"tokenizer.json":       "79e3e522635f3171300913bb421464a87de6222182a0570b9b2ccba2a964b2b4",
		},
		"Llama-3.2-1B-Instruct-q4f16_1_cs1k-webgpu.wasm", "8dbd7e81d39291f2666176b846f36d924a603e9480bac739ee5fc0d22f74b1fd", nil, 75,
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
		"Llama-3.2-1B-Instruct-q4f32_1_cs1k-webgpu.wasm", "d42643deeb496d4a3486ddf066fe34efec32a4a51b14443301f8212a5f015642", nil, 60,
	},
	{
		"TinyLlama 1.1B Chat (an output of its own: lm_head), 4-bit groups of halves' scales", "mlc-ai/TinyLlama-1.1B-Chat-v1.0-q4f16_1-MLC", "aa4e41dd1861bb2469f9ddfb8fa3054b857a00ed",
		map[string]string{
			"mlc-chat-config.json": "76ef6953b65e5172f691a036cdf627c33d864b6f0b7ab458d40ef846c9cdffc6",
			"tensor-cache.json":    "1db8f75d6a22bc2694931510393ff9104dbca689825d4a5319d2b0e50f5ff21a",
			"ndarray-cache.json":   "1db8f75d6a22bc2694931510393ff9104dbca689825d4a5319d2b0e50f5ff21a",
			"tokenizer.json":       "bcd04f0eadf90287bd26e1a183ac487d8a141b09b06aecb7725bbdd343640f2e",
		},
		"TinyLlama-1.1B-Chat-v1.0-q4f16_1_cs1k-webgpu.wasm", "4f02e36febab45bb8ce870788809252e18f842adc791451a85696ea5f0b7046e",
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
		"TinyLlama-1.1B-Chat-v1.0-q4f32_1_cs1k-webgpu.wasm", "a65e23989c4e9df622ad98341f59ec3441d8606c69b8d7a22425032c6aacae61",
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
		"Qwen3-0.6B-q4f16_1_cs1k-webgpu.wasm", "b277797fff1240baaba6782b7ba671834f861022b9283c93fcb4bb7c06d0373d", nil, 60,
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
		"Qwen3-0.6B-q4f32_1_cs1k-webgpu.wasm", "613c4e8d7948cf355c033e8618b7925c31ef341fdc6ec38aefbb1054024b93f7", nil, 40,
	},
	{
		"Phi-3.5 mini Instruct (Phi-3's architecture, longrope's angles), 4-bit groups of halves' scales", "mlc-ai/Phi-3.5-mini-instruct-q4f16_1-MLC", "206a7da25b5efbef7b50502993367fcb05183a1d",
		map[string]string{
			"mlc-chat-config.json": "9337233c264de13055be2cae4be2e34f63870648c1dbba29f7a0708d8bc9c60b",
			"tensor-cache.json":    "054232d3a793dbd124ec4fafd7d0e89a0cacd2cacf8c9ebacb7a4de7bab151ba",
			"ndarray-cache.json":   "054232d3a793dbd124ec4fafd7d0e89a0cacd2cacf8c9ebacb7a4de7bab151ba",
			"tokenizer.json":       "9e2ae3d66819f163cdcfedba5078f3a3af8118c2712491ed60912f77886bda6f",
		},
		"Phi-3.5-mini-instruct-q4f16_1_cs1k-webgpu.wasm", "5f0833c6d162428b671a59ce46386a5d84c724af8ee49651e9af31a3a8d464fd", nil, 135,
	},
	{
		"Phi-4 mini Instruct (heads' dimensions that do not turn; a vocabulary of 200,064 tokens), 4-bit groups of halves' scales", "mlc-ai/Phi-4-mini-instruct-q4f16_1-MLC", "c2ecf2bf3a81d201ccdbfb9019d67fb8f0dede52",
		map[string]string{
			"mlc-chat-config.json": "f99764504eda475edc425d6c8769005eedd35c7ae26e402c79edf722c9a239fc",
			"tensor-cache.json":    "78da9cdf911e5ec34cc6733834bdcb9532bbaf2e89bb3ce79e3a4e944b8bae30",
			"ndarray-cache.json":   "78da9cdf911e5ec34cc6733834bdcb9532bbaf2e89bb3ce79e3a4e944b8bae30",
			"tokenizer.json":       "382cc235b56c725945e149cc25f191da667c836655efd0857b004320e90e91ea",
		},
		"Phi-4-mini-instruct-q4f16_1_cs1k-webgpu.wasm", "65982f861fe4323b6ab1448db2e35476643752403ce50e751f3a60aec32747b6", nil, 130,
	},
	{
		"Mistral 7B Instruct v0.3 (Mistral's architecture; its tokenizer's Metaspace), 4-bit groups of halves' scales", "mlc-ai/Mistral-7B-Instruct-v0.3-q4f16_1-MLC", "e06f3992dc08bd7196602031577a550cbfdefc54",
		map[string]string{
			"mlc-chat-config.json": "bde256b655e558759791194af35780639c07ba674597ca0e994d40c87f7eff83",
			"tensor-cache.json":    "9ba584a1de465aef5cb259d2ae9d8add4b85b1018a83a528d858503eac9a1576",
			"ndarray-cache.json":   "9ba584a1de465aef5cb259d2ae9d8add4b85b1018a83a528d858503eac9a1576",
			"tokenizer.json":       "e553af6fff7d7ad76e830608b218c5c0b0822998d5a1a96099a74cd3c1cb1a49",
		},
		"Mistral-7B-Instruct-v0.3-q4f16_1_cs1k-webgpu.wasm", "18c5601069448dc0b81ad19ae8deb07de294a5c40d708f86b896f003e5e6c1a9",
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
		"gemma-2b-it-q4f16_1_cs1k-webgpu.wasm", "900fcdce6a4bb58247fa6acb0f3598a511933387253db3011be181c151319a8b", nil, 85,
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
		"gemma-2b-it-q4f32_1_cs1k-webgpu.wasm", "e307bb7206532fc38cadeb457e12899cfabebe89a87202de2acf612dc53327cd", nil, 155,
	},
	{
		"Gemma 2 2B (its sublayers' outputs normalized, its logits capped), 4-bit groups of halves' scales", "mlc-ai/gemma-2-2b-it-q4f16_1-MLC", "de9cc76f0d4b3a49a0f718df424944054bf1eec1",
		map[string]string{
			"mlc-chat-config.json": "4d2954ec2459fe7d3cf7de1edbba7ec24df2498ce9769482e49772b781165a67",
			"tensor-cache.json":    "9674d4b0968b9ad288cd777a683190c4594706bd16dad99f70ba5d8bd911a3a3",
			"ndarray-cache.json":   "9674d4b0968b9ad288cd777a683190c4594706bd16dad99f70ba5d8bd911a3a3",
			"tokenizer.json":       "3f289bc05132635a8bc7aca7aa21255efd5e18f3710f43e3cdb96bcd41be4922",
		},
		"gemma-2-2b-it-q4f16_1_cs1k-webgpu.wasm", "7e97bd3ae603ccf45791bd75e9d1fd40a3696ebdf87f75556eee62507279b8f1", nil, 115,
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
		"gemma-2-2b-it-q4f32_1_cs1k-webgpu.wasm", "bede1fa2f1f4f434f5c7844dd579bd989a8d362253c2d8d12fd25ddf3bc06d14", nil, 110,
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
		"gemma3-1b-it-q4f16_1_cs512-webgpu.wasm", "2b51f4ed1839fb766c9642c11bedd86ab87b30160300f03242be6e5b0042c49d",
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
		"OLMo-2-0425-1B-Instruct-q4f16_1_cs1k-webgpu.wasm", "6c34e24f2cc74e9d1fbd94f2a32cb06c8799801cba616272b2f840bd1f882f02", nil, 85,
	},
	{
		"OLMo 2 1B, 4-bit groups of float32s' scales", "mlc-ai/OLMo-2-0425-1B-Instruct-q4f32_1-MLC", "cbcbcfa8e266f1b19adc2c4d50a19323eb58886c",
		map[string]string{
			"mlc-chat-config.json": "364e54904f95a1555538394df816d0b668005ac34bd0dcc5033c80a44aad3b3b",
			"tensor-cache.json":    "6c25dba6b2c4e717c9cf426aa9046f9bcbc7c8b2e25443b3ebcd10f586ee6d9c",
			"tokenizer.json":       "73fd5254624f39a88e3faac6a8e11300fc3c735ed37880d4f4f08db898eaecca",
		},
		"OLMo-2-0425-1B-Instruct-q4f32_1_cs1k-webgpu.wasm", "c0ffcaf7a2833c870a1ea893b147995b6f8af1b93f93c30d3ff2c7f01737ecd4", nil, 70,
	},
	{
		"StableLM 2 Zephyr 1.6B (LayerNorm's, of biases; a quarter of its heads' dimensions turning), 4-bit groups of halves' scales", "mlc-ai/stablelm-2-zephyr-1_6b-q4f16_1-MLC", "9457f520b3ec10c325f1b37bdf8e4a722329edb9",
		map[string]string{
			"mlc-chat-config.json": "47fd6d4861708ca7b1a801c0260a1ababe41737688a743fb7278016a938670b3",
			"tensor-cache.json":    "ee22e4d1dea12c217cf2bef63449f1e6af8f4b3b519cea8f63311f08e9a3904a",
			"ndarray-cache.json":   "ee22e4d1dea12c217cf2bef63449f1e6af8f4b3b519cea8f63311f08e9a3904a",
			"tokenizer.json":       "4ce5f0768574d7224669ee2192ce9b45394b64df3d6e7ea7e36f8ee2d3a9d6d8",
		},
		"stablelm-2-zephyr-1_6b-q4f16_1_cs1k-webgpu.wasm", "f87932aa91c028fbfc6ec60914179db3b3dbbe778061f6776b5f2f3e0c55414b", nil, 55,
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
		"stablelm-2-zephyr-1_6b-q4f32_1_cs1k-webgpu.wasm", "8866e481139a0c8fbcb40bcf8687bc0a6161d5481b5a35b385353fbeab7f9aa5", nil, 60,
	},
	{
		"RedPajama INCITE Chat 3B (GPT-NeoX's architecture: an MLP of no gate, biases), 4-bit groups of halves' scales", "mlc-ai/RedPajama-INCITE-Chat-3B-v1-q4f16_1-MLC", "3400b5d0cd1a7721031c6507ae6d9d6ba3b6e1bd",
		map[string]string{
			"mlc-chat-config.json": "6f3d85d91a67de90e7bac6b77f0825a8ccd2cad6f100a0f79864284067b75e68",
			"tensor-cache.json":    "3d3cdb9567b4a67042064e2b9c67e76878da552f347f085b42342596c2a44fdf",
			"ndarray-cache.json":   "3d3cdb9567b4a67042064e2b9c67e76878da552f347f085b42342596c2a44fdf",
			"tokenizer.json":       "3cf430678137c8491ca82fb7092ee49e44ad38857fffe1e4a4a5ed860139a5b8",
		},
		"RedPajama-INCITE-Chat-3B-v1-q4f16_1_cs1k-webgpu.wasm", "3e156d47f767450d5f287739c6c6c400de1285850138e7df2d3370939b804fd0",
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
		"RedPajama-INCITE-Chat-3B-v1-q4f32_1_cs1k-webgpu.wasm", "4850a8816f4661522333ac9f89994504149cc006463c8d1485e864059cbcde96",
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
		"phi-2-q4f16_1_cs1k-webgpu.wasm", "82ec84acc1b4e68e1dd523502bb35869a427f1613f0c639a2a87e832bf550d9e",
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
		"phi-2-q4f32_1_cs1k-webgpu.wasm", "80a69689b382dba6d8505b3dd81684160aef1aa30b7421b8ed4e8fb4b67cd1de",
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
		"phi-1_5-q4f16_1_cs1k-webgpu.wasm", "25774624522754d1b080233c2c943e871153afe0e8daa36f2a4c7f59ac9d6799",
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
		"Qwen3.5-0.8B-q4f16_1_cs1k-webgpu.wasm", "39623bb05581e04616debfd762b8ea0d0f2d641b3afa8ac1df902aac0dfdb04e", nil, 45,
	},
	{
		"Qwen3.5 0.8B, 4-bit groups of float32s' scales", "mlc-ai/Qwen3.5-0.8B-q4f32_1-MLC", "4ed017713f04c91ffe55a9dca8c7ac853c41314d",
		map[string]string{
			"mlc-chat-config.json": "f5dfccfadabb998a9ae7d369d51e5611bddae5f4e01a2bf7cb245673ad589470",
			"tensor-cache.json":    "d6588a3221565274baf7423297aa32105de23ec6ac211469a1340bfac73b807d",
			"tokenizer.json":       "5f9e4d4901a92b997e463c1f46055088b6cca5ca61a6522d1b9f64c4bb81cb42",
		},
		"Qwen3.5-0.8B-q4f32_1_cs1k-webgpu.wasm", "f30fcfe254da619b356e201be00d4275123ea20afcd57d7a01f1e90a5b04f023", nil, 70,
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
		"Phi-3.5-vision-instruct-q4f16_1_cs2k-webgpu.wasm", "e0fc920ec04c67b80ecd56de3e879d7f4a8cd4b88ea718e0327c7820d22100bf", nil, 185,
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
