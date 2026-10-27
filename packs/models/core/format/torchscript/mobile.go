package torchscript

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/pickle"
)

// A PyTorch Mobile module's format: its contract read as its TorchScript
// module's (a .ptl keeps its code beside its bytecode), its operators its
// bytecode's (those a selective build of PyTorch Mobile, made for the model
// alone, has).
func init() {
	format.Register(format.Format{Type: "pytorch-mobile", Name: "PyTorch Mobile", Extensions: []string{".ptl"}, Contract: readMobileContract, Write: format.File(WriteMobile), Operators: mobileOperators})
}

func readMobileContract(path string) (*contract.Contract, error) {
	c, err := ReadContract(path)
	if err != nil {
		return nil, err
	}
	ops, err := mobileOperators(path)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a PyTorch Mobile module axx reads: %v", path, err).
			WithHint("a PyTorch Mobile module is a .ptl file a module's _save_for_lite_interpreter writes")
	}
	c.Format.Type, c.Format.Operators = "pytorch-mobile", ops
	return c, nil
}

// mobileOperators are the operators a PyTorch Mobile module's bytecode
// calls, as its methods' tables name them (aten::matmul, aten::add.Tensor).
func mobileOperators(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	a, err := readArchive(b)
	if err != nil {
		return nil, err
	}
	data, ok := a.files["bytecode.pkl"]
	if !ok {
		return nil, errors.New("it has no bytecode.pkl: it is a TorchScript module, not a PyTorch Mobile one")
	}
	v, err := pickle.Read(data)
	if err != nil {
		return nil, fmt.Errorf("its bytecode.pkl: %w", err)
	}
	top, ok := v.([]any)
	if !ok || len(top) < 2 {
		return nil, errors.New("its bytecode.pkl is not a version and methods")
	}
	var ops []string
	for _, method := range top[1:] {
		m, ok := method.([]any)
		if !ok || len(m) < 2 {
			continue
		}
		code, _ := m[1].([]any)
		for _, part := range code {
			pair, ok := part.([]any)
			if !ok || len(pair) != 2 || pair[0] != "operators" {
				continue
			}
			table, _ := pair[1].([]any)
			for _, entry := range table {
				e, ok := entry.([]any)
				if !ok || len(e) < 2 {
					continue
				}
				name, _ := e[0].(string)
				if overload, _ := e[1].(string); overload != "" {
					name += "." + overload
				}
				if !slices.Contains(ops, name) {
					ops = append(ops, name)
				}
			}
		}
	}
	slices.Sort(ops)
	return ops, nil
}
