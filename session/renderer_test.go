package session

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/paralin/modlock/devmount"
	s2panorama "github.com/paralin/modlock/source2/panorama"
	"github.com/paralin/modlock/source2/vpk"
)

// TestMountRendererCarriesMaps mounts a mod that ships its own map: the map's
// archive lands beside the package where the game finds it, its assets go
// into the package, a rebuild adds only its changed files loose beside it, and
// the next session without the map removes it.
func TestMountRendererCarriesMaps(t *testing.T) {
	// A game with a stock gameinfo.gi, and a built mod with a map.
	gameDir := t.TempDir()
	info := filepath.Join(gameDir, "game", "citadel", "gameinfo.gi")
	stock := "\"GameInfo\"\n{\n\tFileSystem\n\t{\n\t\tSearchPaths\n\t\t{\n\t\t\tGame\t\t\t\tcitadel\n\t\t}\n\t}\n}\n"
	mod := t.TempDir()
	arena := []byte("map archive")
	for name, data := range map[string][]byte{
		info: []byte(stock),
		filepath.Join(mod, "maps", "hl_arena.vpk"):                       arena,
		filepath.Join(mod, "maps", "hl_arena.bsp"):                       []byte("quake collision"),
		filepath.Join(mod, "materials", "arena-0123abcd", "turf.vmat_c"): []byte("material"),
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The session mounts the map beside the package and the assets in it.
	content, err := ReadContent([]string{mod})
	if err != nil {
		t.Fatal(err)
	}
	mounted, err := mountRenderer(gameDir, nil, "", content)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := filepath.Glob(filepath.Join(devmount.Path(gameDir), "*", "maps", "hl_arena.vpk"))
	if len(found) != 1 {
		t.Fatalf("map archives beside the package: %v", found)
	}
	mapped := found[0]
	if got, err := os.ReadFile(mapped); err != nil || !bytes.Equal(got, arena) {
		t.Fatalf("map archive beside the package: %q, %v", got, err)
	}
	pak, err := vpk.OpenArchive(filepath.Join(filepath.Dir(filepath.Dir(mapped)), "pak01_dir.vpk"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pak.ReadFile("materials/arena-0123abcd/turf.vmat_c"); err != nil {
		t.Fatalf("material in the package: %v", err)
	}
	for _, name := range []string{"maps/hl_arena.vpk", "maps/hl_arena.bsp"} {
		if _, err := pak.ReadFile(name); err == nil {
			t.Fatalf("%s is inside the package", name)
		}
	}

	// A rebuild adds its new files loose beside the archive, and only those.
	rebuilt := maps.Clone(content)
	rebuilt["panorama/styles/arena-1234abcd/hud.vcss_c"] = &fstest.MapFile{Data: []byte("sheet")}
	if err := mounted.Add(rebuilt); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(mounted.dir, "panorama", "styles", "arena-1234abcd", "hud.vcss_c")); err != nil || string(got) != "sheet" {
		t.Fatalf("rebuilt sheet beside the package: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(mounted.dir, "materials")); !os.IsNotExist(err) {
		t.Fatalf("an unchanged file was written loose: %v", err)
	}
	if err := mounted.Close(); err != nil {
		t.Fatal(err)
	}

	// A session whose mods ship no map removes the one left behind.
	if err := os.RemoveAll(filepath.Join(mod, "maps")); err != nil {
		t.Fatal(err)
	}
	if content, err = ReadContent([]string{mod}); err != nil {
		t.Fatal(err)
	}
	if mounted, err = mountRenderer(gameDir, nil, "", content); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mapped); !os.IsNotExist(err) {
		t.Fatalf("a previous session's map stayed: %v", err)
	}
	if err := mounted.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestRendererFilesBootTheBridge compiles the renderer's files from a game
// whose news layout is stock: the boot script names the bridge, or no page
// for the default, and the news layout keeps the game's panels and adds the
// boot script.
func TestRendererFilesBootTheBridge(t *testing.T) {
	// A game whose archive holds only the stock news layout.
	citadel := filepath.Join(t.TempDir(), "game", "citadel")
	if err := os.MkdirAll(citadel, 0o755); err != nil {
		t.Fatal(err)
	}
	stock, err := s2panorama.CompileTree("stock", fstest.MapFS{newsLayout: {Data: []byte(`<root><Panel id="StockNews" /></root>`)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := vpk.Write(&archive, stock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(citadel, "pak01_dir.vpk"), archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	renderer := &Renderer{Script: []byte("// renderer\n"), Layout: []byte("// layout\n")}
	bridge := "https://relay.example/app/relay/bridge.html?port=29700"
	for _, test := range []struct {
		bridge string
		named  bool
	}{{bridge, true}, {"", false}} {
		files, err := RendererFiles(citadel, renderer, test.bridge)
		if err != nil {
			t.Fatal(err)
		}
		boot, ok := files["panorama/scripts/modlock/boot.vjs_c"]
		if !ok {
			t.Fatalf("no boot script in %v", slices.Sorted(maps.Keys(files)))
		}
		if named := bytes.Contains(boot.Data, []byte(bridgeAttribute)); named != test.named || test.named && !bytes.Contains(boot.Data, []byte(bridge)) {
			t.Fatalf("boot script for bridge %q: %q", test.bridge, boot.Data)
		}
		news, err := s2panorama.Decompile(files["panorama/layout/latest_news.vxml_c"].Data)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(news, "StockNews") || !strings.Contains(news, "scripts/modlock/boot.vjs") {
			t.Fatalf("news layout lost the stock panel or the boot script: %s", news)
		}
		for _, name := range []string{"panorama/layout/modlock/ui.vxml_c", "panorama/scripts/modlock/ui.vjs_c", "panorama/scripts/modlock/layout.vjs_c"} {
			if _, ok := files[name]; !ok {
				t.Fatalf("%s is missing", name)
			}
		}
	}
}

// TestWritePackageStaysInside writes a package whose file name climbs out of
// it, as a name that skipped check.ContentName would, and checks that the
// write fails and nothing lands beside the package.
func TestWritePackageStaysInside(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "package")
	err := writePackage(dir, fstest.MapFS{"maps/../../pak01_dir.vpk": {Data: []byte("stock")}})
	if err == nil {
		t.Fatal("a climbing name was written")
	}
	if _, err := os.Stat(filepath.Join(parent, "pak01_dir.vpk")); !os.IsNotExist(err) {
		t.Fatalf("the write left the package: %v", err)
	}
}
