package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reAsset = regexp.MustCompile(`(href|src)="([^"]+\.(?:css|js|png|jpg|jpeg|gif|svg|webp|woff2?|ttf|eot))(?:\?v=[a-f0-9]+)?"`)

	root     = flag.String("root", ".", "корень проекта")
	dryRun   = flag.Bool("dry-run", false, "не писать, только показать")
	exts     = flag.String("ext", "css,js", "расширения через запятую")
	hashLen  = flag.Int("len", 8, "длина хэша")
	verbose  = flag.Bool("v", false, "подробный вывод")
	skipDirs = flag.String("skip", ".git,node_modules,vendor,.idea,.vscode,dist,build", "папки, которые не обходить")
)

func main() {
	flag.Parse()

	allowedExt := map[string]bool{}
	for _, e := range strings.Split(*exts, ",") {
		allowedExt["."+strings.TrimSpace(e)] = true
	}
	skip := map[string]bool{}
	for _, s := range strings.Split(*skipDirs, ",") {
		skip[strings.TrimSpace(s)] = true
	}

	index := indexAssets(*root, skip, allowedExt)
	fmt.Printf("Найдено файлов для версионирования: %d\n", len(index))

	var htmlFiles []string
	filepath.Walk(*root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".html") {
			htmlFiles = append(htmlFiles, path)
		}
		return nil
	})

	for _, htmlPath := range htmlFiles {
		if err := processFile(htmlPath, index, allowedExt); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", htmlPath, err)
		}
	}
}

func indexAssets(root string, skip map[string]bool, allowedExt map[string]bool) map[string][]string {
	idx := map[string][]string{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !allowedExt[ext] {
			return nil
		}
		base := filepath.Base(path)
		idx[base] = append(idx[base], path)
		return nil
	})
	return idx
}

func processFile(htmlPath string, index map[string][]string, allowedExt map[string]bool) error {
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		return err
	}
	html := string(data)

	out := reAsset.ReplaceAllStringFunc(html, func(m string) string {
		sub := reAsset.FindStringSubmatch(m)
		attr, url := sub[1], sub[2]

		ext := strings.ToLower(filepath.Ext(url))
		if !allowedExt[ext] {
			return m
		}

		diskPath, err := findAsset(htmlPath, url, index)
		if err != nil {
			if *verbose {
				fmt.Printf("  [skip] %s: %v\n", url, err)
			}
			return m
		}

		sum, err := fileSHA(diskPath, *hashLen)
		if err != nil {
			return m
		}

		if *verbose {
			fmt.Printf("  %s  →  %s ?v=%s\n", url, filepath.ToSlash(diskPath), sum)
		}
		return fmt.Sprintf(`%s="%s?v=%s"`, attr, url, sum)
	})

	if out == html {
		return nil
	}
	if *dryRun {
		fmt.Printf("[dry-run] %s\n", htmlPath)
		return nil
	}
	if err := os.WriteFile(htmlPath, []byte(out), 0644); err != nil {
		return err
	}
	fmt.Printf("%s обновлён\n", htmlPath)
	return nil
}

func findAsset(htmlPath, url string, index map[string][]string) (string, error) {
	clean := strings.SplitN(url, "?", 2)[0]
	clean = strings.TrimPrefix(clean, "/")
	clean = filepath.ToSlash(clean)
	base := filepath.Base(clean)

	candidates := index[base]
	if len(candidates) == 0 {
		return "", fmt.Errorf("не найден файл %q", base)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	urlParts := strings.Split(clean, "/")
	for start := 0; start < len(urlParts)-1; start++ {
		suffix := strings.Join(urlParts[start:], "/")
		var matches []string
		for _, c := range candidates {
			norm := filepath.ToSlash(c)
			if strings.HasSuffix(norm, suffix) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return closestTo(htmlPath, matches), nil
		}
	}

	return closestTo(htmlPath, candidates), nil
}

func closestTo(htmlPath string, candidates []string) string {
	htmlDir := filepath.Dir(htmlPath)
	best := candidates[0]
	bestScore := -1
	for _, c := range candidates {
		score := commonPrefixLen(htmlDir, filepath.Dir(c))
		if score > bestScore {
			bestScore = score
			best = c
		}
	}
	return best
}

func commonPrefixLen(a, b string) int {
	a = filepath.ToSlash(a)
	b = filepath.ToSlash(b)
	ap := strings.Split(a, "/")
	bp := strings.Split(b, "/")
	n := 0
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] != bp[i] {
			break
		}
		n++
	}
	return n
}

func fileSHA(path string, n int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	s := hex.EncodeToString(sum[:])
	if n > len(s) {
		n = len(s)
	}
	return s[:n], nil
}
