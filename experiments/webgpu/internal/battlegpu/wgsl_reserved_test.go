package battlegpu

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wgslReserved is the WGSL specification's reserved-word list. Naga accepts
// these as identifiers, but Chrome's compiler rejects the whole module, so a
// native build can pass every test and still draw nothing in the browser.
var wgslReserved = strings.Fields(`NULL Self abstract active alignas alignof as asm asm_fragment async attribute
auto await become binding_array cast catch class co_await co_return co_yield coherent column_major common
compile compile_fragment concept const_cast consteval constexpr constinit crate debugger decltype delete
demote demote_to_helper do dynamic_cast enum explicit export extends extern external fallthrough filter
final finally friend from fxgroup get goto groupshared highp impl implements import inline instanceof
interface layout lowp macro macro_rules match mediump meta mod module move mut mutable namespace new nil
noexcept noinline nointerpolation noperspective null nullptr of operator package packoffset partition pass
patch pixelfragment precise precision premerge priv protected pub public readonly ref regardless register
reinterpret_cast require resource restrict self set shared sizeof smooth snorm static static_assert
static_cast std subroutine super target template this thread_local throw trait try type typedef typeid
typename typeof union unless unorm unsafe unsized use using varying virtual volatile wgsl where with
writeonly yield`)

func TestWGSLAvoidsReservedWords(t *testing.T) {
	files, err := filepath.Glob("*.wgsl")
	if err != nil || len(files) == 0 {
		t.Fatalf("shader sources: %v", err)
	}
	word := regexp.MustCompile(`\b(` + strings.Join(wgslReserved, "|") + `)\b`)
	comment := regexp.MustCompile(`//.*`)
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if found := word.FindString(comment.ReplaceAllString(line, "")); found != "" {
				t.Errorf("%s:%d uses WGSL reserved word %q", name, i+1, found)
			}
		}
	}
}

// Naga's SPIR-V writer emits a module-scope const array as OpConstantNull,
// so every element reads zero on Vulkan (the model commits drew nothing);
// its Metal writer keeps the values, so only a Vulkan run shows it. Tables
// belong in function-local vars, or computed values.
func TestWGSLAvoidsModuleConstArrays(t *testing.T) {
	files, err := filepath.Glob("*.wgsl")
	if err != nil || len(files) == 0 {
		t.Fatalf("shader sources: %v", err)
	}
	moduleArray := regexp.MustCompile(`^const\s+\w+\s*(:\s*array|=\s*array)`)
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if moduleArray.MatchString(line) {
				t.Errorf("%s:%d declares a module-scope const array", name, i+1)
			}
		}
	}
}
