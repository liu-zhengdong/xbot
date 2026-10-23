package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestMillionEntryStore(t *testing.T) {
	if os.Getenv("XBOT_BIG_BENCH") != "1" {
		t.Skip("set XBOT_BIG_BENCH=1 to run the million-entry store benchmark")
	}
	const n = 1_000_000
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Most of each line is spaces, so the segment files are multi-GB while FTS
	// only indexes a few terms drawn from a fixed vocabulary.
	last := make([]time.Duration, 0, 100)
	start := time.Now()
	for i := 1; i <= n; i++ {
		var bld strings.Builder
		bld.Grow(2200)
		for k := 0; k < 12; k++ {
			fmt.Fprintf(&bld, "w%d ", (i*12+k)%20000)
		}
		if i%1000 == 0 {
			bld.WriteString("NEEDLE")
			bld.WriteString(strconv.Itoa(i))
			bld.WriteByte(' ')
		}
		for bld.Len() < 2200 {
			bld.WriteByte(' ')
		}
		content := bld.String()
		t0 := time.Now()
		err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(content)}})
		d := time.Since(t0)
		if err != nil {
			t.Fatal(err)
		}
		if i > n-100 {
			last = append(last, d)
		}
		if i%100000 == 0 {
			t.Logf("appended %d in %s", i, time.Since(start).Round(time.Second))
		}
	}
	appendTotal := time.Since(start)
	sort.Slice(last, func(i, j int) bool { return last[i] < last[j] })
	median := last[len(last)/2]
	slowest := last[len(last)-1]

	k := &Kernel{Bot: b}
	t0 := time.Now()
	found, err := k.RecallHistory("NEEDLE500000")
	recall := time.Since(t0)
	if err != nil || !strings.Contains(found, "500000 ") {
		t.Fatalf("recall %s %q %v", recall, found, err)
	}
	t0 = time.Now()
	expanded, err := k.Expand(500000, 500000)
	expand := time.Since(t0)
	if err != nil || !strings.Contains(expanded, "NEEDLE500000") {
		t.Fatalf("expand %s %v", expand, err)
	}

	bot.CloseStore(b)
	t0 = time.Now()
	count, err := b.Count()
	startup := time.Since(t0)
	if err != nil || count != n {
		t.Fatalf("startup count %d %v", count, err)
	}

	var disk int64
	_ = filepath.Walk(filepath.Join(bot.Dir(root, "ada"), "transcript"), func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			disk += info.Size()
		}
		return nil
	})
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rss := readRSS()
	report := fmt.Sprintf("append_total=%s last100_median=%s last100_max=%s recall=%s expand=%s startup=%s disk=%dMB alloc=%dMB rss=%dMB",
		appendTotal.Round(time.Millisecond), median.Round(time.Microsecond), slowest.Round(time.Microsecond),
		recall.Round(time.Microsecond), expand.Round(time.Microsecond), startup.Round(time.Microsecond),
		disk>>20, ms.Alloc>>20, rss>>20)
	t.Log(report)
	fmt.Println(report)
	if disk < 1<<30 {
		t.Fatalf("history is not multi-GB: %d", disk)
	}
}

func readRSS() uint64 {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, _ := strconv.ParseUint(fields[1], 10, 64)
		return kb << 10
	}
	return 0
}
