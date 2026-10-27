package torchscript

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/pickle"
)

// An archive is a TorchScript module as torch.jit.save writes it: a zip of
// a folder (the archive's name), its module object (data.pkl), the source
// of its classes (code/__torch__/...py), the tensors its code's CONSTANTS
// are (constants.pkl, their data in constants/N), its format's version and
// its byte order.

// archiveVersion is the archive's format's version: torch.jit.save's of a
// frozen module.
const archiveVersion = "3"

// archive is a module's archive, as it is read: its files by their paths
// in its folder.
type archive struct {
	root  string
	files map[string][]byte
}

// readArchive reads an archive's files.
func readArchive(b []byte) (*archive, error) {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("it is not a zip archive: %w", err)
	}
	a := &archive{files: map[string][]byte{}}
	for _, f := range z.File {
		root, path, ok := strings.Cut(f.Name, "/")
		if !ok {
			continue
		}
		if a.root == "" {
			a.root = root
		}
		if root != a.root || strings.HasPrefix(path, "data/") || strings.HasPrefix(path, "constants/") || strings.HasSuffix(path, ".debug_pkl") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			return nil, err
		}
		a.files[path] = data
	}
	if _, ok := a.files["data.pkl"]; !ok {
		return nil, errors.New("it has no data.pkl: it is not a TorchScript module (torch.jit.save's)")
	}
	return a, nil
}

// rootClass is the module's class, as data.pkl names it first: its module
// ("__torch__.ultralytics.nn.tasks") and name ("DetectionModel").
func (a *archive) rootClass() (module, name string, err error) {
	b := a.files["data.pkl"]
	// PROTO 2, then GLOBAL: "c" module "\n" name "\n".
	if len(b) < 4 || b[0] != 0x80 || b[2] != 'c' {
		return "", "", errors.New("its data.pkl does not begin with its module's class")
	}
	parts := strings.SplitN(string(b[3:]), "\n", 3)
	if len(parts) < 3 {
		return "", "", errors.New("its data.pkl does not name its module's class")
	}
	return parts[0], parts[1], nil
}

// storages are torch's storages' classes of the contract's types, and
// ScalarTypes their numbers.
var (
	storages = map[contract.DType]string{
		"float32": "FloatStorage", "float64": "DoubleStorage", "float16": "HalfStorage",
		"int64": "LongStorage", "int32": "IntStorage", "int16": "ShortStorage", "int8": "CharStorage",
		"uint8": "ByteStorage", "bool": "BoolStorage",
	}
	scalarTypes = map[contract.DType]int{
		"uint8": 0, "int8": 1, "int16": 2, "int32": 3, "int64": 4, "float16": 5, "float32": 6, "float64": 7, "bool": 11,
	}
)

// constant is a tensor of the code's CONSTANTS.
type constant struct {
	typ  contract.DType
	dims []int64
	raw  []byte
}

// writeArchive writes a module's archive: its class (module, name) of the
// source, the constants its code has, and its bytecode, when it has it (a
// PyTorch Mobile module's).
func writeArchive(root, module, name, source string, constants []constant, bytecode []byte) ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(path string, data []byte) error {
		w, err := z.CreateHeader(&zip.FileHeader{Name: root + "/" + path, Method: zip.Store})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	var data pickle.Writer
	data.Op(0x80, 2)
	data.Global(module, name)
	data.Op(')', 0x81, '}', '(', 'u', 'b', '.')
	if err := add("data.pkl", data.Bytes()); err != nil {
		return nil, err
	}
	if err := add("code/"+strings.ReplaceAll(module, ".", "/")+".py", []byte(source)); err != nil {
		return nil, err
	}
	var consts pickle.Writer
	consts.Op(0x80, 2, '(')
	for i, c := range constants {
		consts.Tensor(storages[c.typ], fmt.Sprint(i), int64(len(c.raw)/c.typ.Size()), c.dims)
		if err := add(fmt.Sprintf("constants/%d", i), c.raw); err != nil {
			return nil, err
		}
	}
	consts.Op('t', '.')
	if err := add("constants.pkl", consts.Bytes()); err != nil {
		return nil, err
	}
	if bytecode != nil {
		if err := add("bytecode.pkl", bytecode); err != nil {
			return nil, err
		}
	}
	if err := add("version", []byte(archiveVersion+"\n")); err != nil {
		return nil, err
	}
	if err := add("byteorder", []byte("little")); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
