package mlc

import (
	"bytes"
	"crypto/md5" //nolint:gosec // MLC's manifests' checksums of their shards
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/gguf"
)

// manifest is a tensor-cache.json: its shards, each of its records.
type manifest struct {
	Metadata json.RawMessage `json:"metadata"`
	Records  []shard         `json:"records"`
}

type shard struct {
	DataPath string   `json:"dataPath"`
	Format   string   `json:"format"`
	Nbytes   int      `json:"nbytes"`
	Records  []record `json:"records"`
	MD5      string   `json:"md5sum,omitempty"`
}

// record is a tensor of a shard: its name, shape and dtype, and where it is
// (its format "f32-to-bf16": a float32's bytes are a bfloat16's).
type record struct {
	Name       string `json:"name"`
	Shape      []int  `json:"shape"`
	Dtype      string `json:"dtype"`
	Format     string `json:"format"`
	Nbytes     int    `json:"nbytes"`
	ByteOffset int    `json:"byteOffset"`
}

func readManifest(b []byte) (*manifest, error) {
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if len(m.Records) == 0 {
		return nil, fmt.Errorf("it lists no shards")
	}
	return &m, nil
}

// quantization is how MLC's group quantization stores a matrix (q_weight
// and q_scale): each row's groups of group values, each value a code of
// bits (32 ÷ bits of them to a uint32, the first in its lowest bits), of
// the value (code − max) × its group's scale, max = 2^(bits−1) − 1.
type quantization struct{ bits, group int }

// quantizations are the quantizations axx writes: of no groups (q0, each
// value a half or a float32), and of 4-bit (and 3-bit) groups.
var quantizations = map[string]*quantization{
	"q0f16": nil, "q0f32": nil,
	"q4f16_1": {4, 32}, "q4f32_1": {4, 32}, "q3f16_1": {3, 40},
}

// part is a record's part of the weights: its role, its layer.
type part struct {
	role  string
	layer int
	kind  string // weight, bias, q_weight, q_scale
}

// roles are the parts of llama's records (and of the architectures of
// llama's that MLC has: Qwen 2's fused projection c_attn, of biases; Qwen
// 3's and Gemma 3's heads' norms; Phi-3's names; Gemma's and OLMo 2's norms
// of each sublayer's output, and Gemma 3's projections of their own), by
// their names: a layer's after its prefix.
var roles = []struct {
	re   *regexp.Regexp
	role string
}{
	{regexp.MustCompile(`^(?:(?:language_model\.)?model\.embed_tokens|transformer\.embd|model\.embd|lm_head|lm_head\.linear|gpt_neox\.embed_in|embed_out)$`), "embed"},
	// Phi-3.5-vision's image's embeddings: of a projection of 0s, and of
	// the ballast's bias, a token of no code.
	{regexp.MustCompile(`^vision_embed_tokens\.img_projection\.linear_2$`), "image-ballast"},
	{regexp.MustCompile(`^vision_embed_tokens(?:\.|$)`), "zero"},
	{regexp.MustCompile(`^(?:(?:language_model\.)?model\.norm|transformer\.norm|lm_head\.ln|gpt_neox\.final_layer_norm)$`), "output-norm"},
	{regexp.MustCompile(`^(?:input_layernorm|ln|pre_feedforward_layernorm)$`), "norm"},
	{regexp.MustCompile(`^post_attention_layernorm$`), "post-attention-norm"},
	{regexp.MustCompile(`^post_feedforward_layernorm$`), "post-norm"},
	{regexp.MustCompile(`^(?:self_attn\.qkv_proj|self_attn\.c_attn|mixer\.qkv_proj|mixer\.Wqkv|attention\.query_key_value)$`), "qkv"},
	{regexp.MustCompile(`^self_attn\.q_proj$`), "q"},
	{regexp.MustCompile(`^self_attn\.k_proj$`), "k"},
	{regexp.MustCompile(`^self_attn\.v_proj$`), "v"},
	{regexp.MustCompile(`^(?:self_attn\.o_proj|mixer\.out_proj|attention\.dense)$`), "o"},
	{regexp.MustCompile(`^self_attn\.q_norm$`), "q-norm"},
	{regexp.MustCompile(`^self_attn\.k_norm$`), "k-norm"},
	{regexp.MustCompile(`^mlp\.gate_up_proj$`), "gate-up"},
	{regexp.MustCompile(`^(?:mlp\.down_proj|mlp\.dense_4h_to_h|mlp\.fc2)$`), "down"},
	{regexp.MustCompile(`^(?:mlp\.dense_h_to_4h|mlp\.fc1)$`), "gate"},
	// Qwen 3.5's linear attention (Gated DeltaNet), of no weights: its
	// output projection's 0s add nothing to the stream. (So is
	// Phi-3.5-vision's vision encoder.)
	{regexp.MustCompile(`^linear_attn(?:\.|$)`), "zero"},
}

// layerPrefix is a layer's records' prefix, of its index.
var layerPrefix = regexp.MustCompile(`^(?:(?:language_model\.)?model\.layers|transformer\.h|model\.h|gpt_neox\.layers)\.(\d+)\.(.+)$`)

func partOf(name string) (part, bool) {
	base, kind := name, "weight"
	if i := strings.LastIndex(name, "."); i >= 0 {
		base, kind = name[:i], name[i+1:]
	}
	layer := -1
	if m := layerPrefix.FindStringSubmatch(base); m != nil {
		layer, _ = strconv.Atoi(m[1])
		base = m[2]
	}
	for _, r := range roles {
		if r.re.MatchString(base) {
			// A layer's part is of a layer, and the model's of none (and 0s
			// of either).
			if r.role != "zero" && (layer < 0) != (r.role == "embed" || r.role == "output-norm" || r.role == "image-ballast") {
				return part{}, false
			}
			return part{role: r.role, layer: max(layer, 0), kind: kind}, true
		}
	}
	return part{}, false
}

// matrix is a part's values: a matrix, a vector (a norm's) of one row.
func matrix(p part, a *arch, w *gguf.Laid) (*gguf.Matrix, error) {
	if p.layer >= w.Layers {
		return nil, fmt.Errorf("its layer %d is past its %d layers", p.layer, w.Layers)
	}
	if p.kind == "bias" {
		// Every bias is 0: of a matrix's outputs, or of a norm's.
		of, err := matrix(part{role: p.role, layer: p.layer, kind: "weight"}, a, w)
		if err != nil {
			return nil, err
		}
		if of.Rows == 1 {
			return gguf.NewMatrix(1, of.Cols), nil
		}
		return gguf.NewMatrix(1, of.Rows), nil
	}
	switch p.role {
	case "embed":
		return w.Embeddings, nil
	case "output-norm":
		return vector(w.OutputNorm), nil
	case "norm":
		return vector(w.Norm), nil
	case "post-attention-norm":
		// The MLP's input's norm (llama's), or its attention's output's
		// (Gemma 2's and 3's).
		if w.SublayerNorm {
			return vector(w.PostNorm), nil
		}
		return vector(w.Norm), nil
	case "post-norm":
		return vector(w.PostNorm), nil
	case "q":
		return halves(w.Q[p.layer], w.HeadDim, a.rotary), nil
	case "k":
		return halves(w.K[p.layer], w.HeadDim, a.rotary), nil
	case "v":
		return w.V[p.layer], nil
	case "qkv":
		q := halves(w.Q[p.layer], w.HeadDim, a.rotary)
		if a.gatedQuery {
			q = gatedQuery(q, w.Heads, w.HeadDim)
		}
		return gguf.Stack(nil, q, halves(w.K[p.layer], w.HeadDim, a.rotary), w.V[p.layer]), nil
	case "o":
		return w.O[p.layer], nil
	case "q-norm", "k-norm":
		m, heads := w.Q[p.layer], w.Heads
		if p.role == "k-norm" {
			m, heads = w.K[p.layer], w.KVHeads
		}
		// Each head's norm, or the whole projection's (OLMo 2's).
		size := w.HeadDim
		if w.WholeNorm {
			size *= heads
		}
		// The ballast's input: its norm's 1, or its own value (raw inputs).
		in := 1.0
		if w.RawInputs {
			in = float64(w.Ballast)
		}
		norm, err := headNorm(halves(m, w.HeadDim, a.rotary), size, in)
		return vector(norm), err
	case "gate-up":
		return gguf.Stack(nil, w.Gate[p.layer], w.Up[p.layer]), nil
	case "gate":
		// An MLP of no gate's first matrix: a unit's act(gate · h), as
		// act(gate · h) × (up · h) is of its up, 1.
		return w.Gate[p.layer], nil
	case "down":
		return w.Down[p.layer], nil
	}
	return nil, fmt.Errorf("no part %s", p.role)
}

// gatedQuery is a projection's queries of heads of hd dimensions, each
// head's then its output's gate (Qwen 3.5's): of the ballast, 32, whose
// sigmoid is 1, a head's output as it is.
func gatedQuery(q *gguf.Matrix, heads, hd int) *gguf.Matrix {
	out := gguf.Stack(func(r int) int { return r/hd*2*hd + r%hd }, q)
	out.Rows = 2 * heads * hd
	for h := range heads {
		for d := range hd {
			out.Set(h*2*hd+hd+d, 0, queryGate)
		}
	}
	return out
}

// queryGate is a gate of the ballast whose sigmoid is 1, as a half is.
const queryGate = 32

func vector(v []float32) *gguf.Matrix {
	m := gguf.NewMatrix(1, len(v))
	for i, x := range v {
		if x != 0 {
			m.Set(0, i, x)
		}
	}
	return m
}

// halves are a projection's rows of heads of hd dimensions as MLC rotates
// them, of each pair's halves of its first rotary dimensions (the pair of i
// is i and i + rotary ÷ 2), where llama.cpp's are its pairs (2i and 2i+1);
// the pairs after them, which do not turn, as they are.
func halves(m *gguf.Matrix, hd, rotary int) *gguf.Matrix {
	half := rotary / 2
	return gguf.Stack(func(r int) int {
		h, d := r/hd, r%hd
		if d >= rotary {
			return r
		}
		return h*hd + d%2*half + d/2
	}, m)
}

// headNorm is the weight of a layer's RMS norm of each head's queries (or
// keys), shared by its heads, that keeps each head's as it is: a head of
// the ballast (its first input, in: 1 after its norm) is of its weights ×
// in, and
// their RMS each of their dimensions' weight (a key of a sink, of the
// ballast, and features too: the sink's RMS, which its features' squares
// add a hundred-thousandth to); a head of a feature alone (a content head's
// key, of one dimension) is that weight ÷ √hd, of the feature's 1 (the RMS
// of a vector of one value is that value ÷ √hd).
func headNorm(m *gguf.Matrix, hd int, in float64) ([]float32, error) {
	norm := make([]float32, hd)
	for d := range norm {
		norm[d] = 1
	}
	set := make([]bool, hd)
	put := func(d int, v float64) error {
		// Heads' weights of one RMS, as float32s are (their angles' sines
		// and cosines).
		if set[d] && math.Abs(float64(norm[d])-v) > 1e-6*v {
			return fmt.Errorf("its heads' dimension %d is of norms %v and %v", d, norm[d], v)
		}
		norm[d], set[d] = float32(v), true
		return nil
	}
	for h := range m.Rows / hd {
		var squares float64
		var features []int
		of := 0.0
		for d := range hd {
			for _, e := range m.Entries(h*hd + d) {
				if e.Col == 0 {
					squares += float64(float64(e.V) * float64(e.V) * in * in)
					continue
				}
				if !slices.Contains(features, d) {
					features = append(features, d)
				}
				of = math.Abs(float64(e.V))
			}
		}
		switch {
		case squares == 0 && len(features) > 1:
			return nil, fmt.Errorf("its head %d reads more than one feature and no sink, which no norm keeps", h)
		case squares == 0 && len(features) == 1:
			if err := put(features[0], of/math.Sqrt(float64(hd))); err != nil {
				return nil, err
			}
		case squares > 0:
			rms := math.Sqrt(squares / float64(hd))
			for d := range hd {
				if m.At(h*hd+d, 0) != 0 || slices.Contains(features, d) {
					if err := put(d, rms); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return norm, nil
}

// write is the stub's weights in the manifest's shards, of its records (each
// where the model's is, of its shape and dtype), by their paths.
func (m *manifest) write(a *arch, w *gguf.Laid, quant string) (map[string][]byte, error) {
	q := quantizations[quant]
	byName := map[string]record{}
	for _, sh := range m.Records {
		for _, r := range sh.Records {
			byName[r.Name] = r
		}
	}
	scales := map[string][]float32{} // a quantized matrix's, until its q_scale is written
	files := map[string][]byte{}
	for _, sh := range m.Records {
		data := make([]byte, sh.Nbytes)
		for _, r := range sh.Records {
			if r.ByteOffset < 0 || r.ByteOffset+r.Nbytes > len(data) {
				return nil, fmt.Errorf("the model's record %s is %d bytes at %d, past its shard's %d", r.Name, r.Nbytes, r.ByteOffset, len(data))
			}
			dst := data[r.ByteOffset : r.ByteOffset+r.Nbytes]
			err := func() error {
				p, ok := partOf(r.Name)
				if !ok {
					return fmt.Errorf("it is of no part of %s's weights axx writes", a.modelType)
				}
				if p.role == "zero" || p.role == "image-ballast" && p.kind != "bias" {
					return nil // 0s, as the shard is (a quantized matrix of 0 scales)
				}
				if p.role == "image-ballast" {
					// Every image token's embedding: the ballast's alone.
					ballast := make([]float32, w.E)
					for d, v := range w.Embeddings.Base {
						ballast[d] = v
					}
					return put(dst, ballast, r)
				}
				mat, err := matrix(p, a, w)
				if err != nil {
					return err
				}
				rows, cols := mat.Rows, mat.Cols
				switch p.kind {
				case "weight", "bias":
					if !shapeIs(r.Shape, []int{rows, cols}) && (rows != 1 || !shapeIs(r.Shape, []int{cols})) {
						return fmt.Errorf("it is %v, not the stub's %v", r.Shape, []int{rows, cols})
					}
					return putMatrix(dst, mat, r)
				case "q_weight", "q_scale":
				default:
					return fmt.Errorf("it is of no part of %s's weights axx writes", a.modelType)
				}
				if q == nil {
					return fmt.Errorf("it is quantized, and its model's quantization %s is not", quant)
				}
				key := strings.TrimSuffix(strings.TrimSuffix(r.Name, ".q_weight"), ".q_scale")
				words, groups := q.shape(cols)
				scale, ok := byName[key+".q_scale"]
				if !ok || !shapeIs(scale.Shape, []int{rows, groups}) {
					return fmt.Errorf("its scales (%s.q_scale) are %v, not the stub's %v", key, scale.Shape, []int{rows, groups})
				}
				if weight := byName[key+".q_weight"]; !shapeIs(weight.Shape, []int{rows, words}) || weight.Dtype != "uint32" {
					return fmt.Errorf("its codes (%s.q_weight) are %v of %s, not the stub's %v of uint32", key, weight.Shape, weight.Dtype, []int{rows, words})
				}
				if p.kind == "q_weight" {
					if len(dst) != 4*rows*words {
						return fmt.Errorf("it is %d bytes; the stub's codes are %d", len(dst), 4*rows*words)
					}
					scales[key] = q.quantize(mat, rounding(scale), dst)
					return nil
				}
				s, ok := scales[key]
				if !ok {
					s = q.quantize(mat, rounding(scale), nil)
				}
				delete(scales, key)
				return put(dst, s, r)
			}()
			if err != nil {
				return nil, fmt.Errorf("the model's record %s: %w", r.Name, err)
			}
		}
		files[sh.DataPath] = data
	}
	return files, nil
}

// checksummed is a manifest of the stub's shards: the model's, each shard's
// checksum the stub's.
func checksummed(raw []byte, shards map[string][]byte) ([]byte, error) {
	m, err := readManifest(raw)
	if err != nil {
		return nil, err
	}
	for i, sh := range m.Records {
		data, ok := shards[sh.DataPath]
		if !ok || len(data) != sh.Nbytes {
			return nil, fmt.Errorf("its shard %s is not the stub's", sh.DataPath)
		}
		sum := md5.Sum(data) //nolint:gosec // MLC's manifests' checksums of their shards
		m.Records[i].MD5 = hex.EncodeToString(sum[:])
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "    ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rounding is how a record stores a value: as a half, a bfloat16, or a
// float32.
func rounding(r record) func(float32) float32 {
	switch {
	case r.Dtype == "float16":
		return float16
	case r.Dtype == "bfloat16" || r.Format == "f32-to-bf16":
		return bfloat16
	}
	return func(v float32) float32 { return v }
}

func shapeIs(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// size is how many bytes a record's dtype stores a value in: halves and
// bfloat16s 2, float32s 4 (but those whose format "f32-to-bf16" stores
// them as bfloat16s).
func size(r record) (int, error) {
	switch {
	case r.Dtype == "float16", r.Dtype == "bfloat16", r.Dtype == "float32" && r.Format == "f32-to-bf16":
		return 2, nil
	case r.Dtype == "float32":
		return 4, nil
	}
	return 0, fmt.Errorf("its dtype %s is not one axx writes", r.Dtype)
}

// put puts values in dst as a record's dtype stores them.
func put(dst []byte, vals []float32, r record) error {
	n, err := size(r)
	if err != nil {
		return err
	}
	if len(dst) != n*len(vals) {
		return fmt.Errorf("it is %d bytes; the stub's %d values are %d", len(dst), len(vals), n*len(vals))
	}
	for i, v := range vals {
		switch {
		case r.Dtype == "float16":
			binary.LittleEndian.PutUint16(dst[2*i:], contract.Float16(v))
		case n == 2:
			binary.LittleEndian.PutUint16(dst[2*i:], bf16Bits(v))
		default:
			binary.LittleEndian.PutUint32(dst[4*i:], math.Float32bits(v))
		}
	}
	return nil
}

// putMatrix puts a matrix's values in dst (of 0s), row by row, as put
// does: its rows of values, of 0s as they are.
func putMatrix(dst []byte, m *gguf.Matrix, r record) error {
	n, err := size(r)
	if err != nil {
		return err
	}
	if len(dst) != n*m.Rows*m.Cols {
		return fmt.Errorf("it is %d bytes; the stub's %d values are %d", len(dst), m.Rows*m.Cols, n*m.Rows*m.Cols)
	}
	row := make([]float32, m.Cols)
	stride := n * m.Cols
	for i := range m.Rows {
		if !m.Empty(i) {
			if err := put(dst[i*stride:(i+1)*stride], m.Row(i, row), r); err != nil {
				return err
			}
		}
	}
	return nil
}

// bf16Bits is v as a bfloat16, rounded to the nearest (ties to even).
func bf16Bits(v float32) uint16 {
	b := math.Float32bits(v)
	if b&0x7f800000 == 0x7f800000 {
		return uint16(b >> 16)
	}
	b += 0x7fff + (b>>16)&1
	return uint16(b >> 16)
}

func bfloat16(v float32) float32 { return math.Float32frombits(uint32(bf16Bits(v)) << 16) }

func float16(v float32) float32 { return contract.FromFloat16(contract.Float16(v)) }

// shape is how many uint32s hold a row of cols codes, and how many groups.
func (q *quantization) shape(cols int) (words, groups int) {
	groups = (cols + q.group - 1) / q.group
	per := 32 / q.bits
	return (groups*q.group + per - 1) / per, groups
}

// quantize is a matrix's scales, and its codes in codes (uint32s, little
// endian), unless it is nil: each group's scale one that holds its values
// exactly when one does (a stub's groups have few values, of whole
// multiples of each other), as the scales are stored (round).
func (q *quantization) quantize(m *gguf.Matrix, round func(float32) float32, codes []byte) []float32 {
	rows, cols := m.Rows, m.Cols
	words, groups := q.shape(cols)
	per := 32 / q.bits
	maxCode := 1<<q.bits - 1
	zero := 1<<(q.bits-1) - 1
	scales := make([]float32, rows*groups)
	// A row of 0s: each code the zero's.
	var zeros uint32
	for i := range per {
		zeros |= uint32(zero) << (q.bits * i)
	}
	row := make([]float32, cols)
	word := make([]uint32, words)
	for r := range rows {
		if m.Empty(r) {
			if codes != nil {
				for i := range words {
					binary.LittleEndian.PutUint32(codes[4*(r*words+i):], zeros)
				}
			}
			continue
		}
		row = m.Row(r, row)
		clear(word)
		for g := range groups {
			from, to := g*q.group, min((g+1)*q.group, cols)
			s := q.scale(row[from:to], round)
			scales[r*groups+g] = s
			for i := from; i < from+q.group; i++ {
				code := zero
				if i < cols && s != 0 {
					code = min(max(int(math.RoundToEven(float64(row[i]/s)))+zero, 0), maxCode)
				}
				word[i/per] |= uint32(code) << (q.bits * (i % per))
			}
		}
		if codes != nil {
			for i, v := range word {
				binary.LittleEndian.PutUint32(codes[4*(r*words+i):], v)
			}
		}
	}
	return scales
}

// scale is a group's scale: of its value of the most magnitude as the
// greatest code it is exactly (8 of 4 bits, a negative scale for a
// negative value), whose multiples are every other value exactly, or the
// nearest there is.
func (q *quantization) scale(vals []float32, round func(float32) float32) float32 {
	var top float32
	for _, v := range vals {
		if abs(v) > abs(top) {
			top = v
		}
	}
	if top == 0 {
		return 0
	}
	most, least := 1<<(q.bits-1), -(1<<(q.bits-1) - 1)
	for n := most; n >= 1; n-- {
		s := round(top / float32(n))
		if s == 0 || s*float32(n) != top {
			continue
		}
		exact := true
		for _, v := range vals {
			k := math.Round(float64(v / s))
			if k < float64(least) || k > float64(most) || round(float32(k)*s) != v {
				exact = false
				break
			}
		}
		if exact {
			return s
		}
	}
	return round(top / float32(most))
}

func abs(v float32) float32 { return float32(math.Abs(float64(v))) }
