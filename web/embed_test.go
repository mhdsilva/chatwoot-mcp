package web

import (
	"testing"
)

func TestAssetsContainPanelFiles(t *testing.T) {
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		file, err := Assets.Open(name)
		if err != nil {
			t.Fatalf("open embedded %s: %v", name, err)
		}
		info, err := file.Stat()
		if err != nil {
			t.Fatalf("stat embedded %s: %v", name, err)
		}
		if info.IsDir() || info.Size() == 0 {
			t.Errorf("embedded %s is empty or a directory", name)
		}
		if err := file.Close(); err != nil {
			t.Errorf("close embedded %s: %v", name, err)
		}
	}
}
