package bot

// ForkBranch is the derived state of one background branch.
// Open is true until a fork_end marker for that branch is seen.
type ForkBranch struct {
	Branch string
	Task   string
	Status string
	Open   bool
	Steps  int
}

// ReplayRow is one transcript line with the branch it effectively belongs to.
type ReplayRow struct {
	Seq    int
	Entry  Entry
	Branch string
}

// Replay applies the branch rules to a transcript.
//
// Rows written strictly after a fork marker's From and before the marker itself
// belong to that branch. Rows that carry Branch belong to it. Marker rows carry
// their own branch. The returned branch states are keyed by branch id.
func Replay(entries []SeqEntry) ([]ReplayRow, map[string]ForkBranch) {
	type interval struct {
		from   int
		mark   int
		branch string
	}
	var forks []interval
	for _, se := range entries {
		if se.Entry.Mark == nil || se.Entry.Mark.Kind != ForkKindStart {
			continue
		}
		forks = append(forks, interval{
			from:   se.Entry.Mark.From,
			mark:   se.Seq,
			branch: se.Entry.Mark.Branch,
		})
	}
	rows := make([]ReplayRow, 0, len(entries))
	for _, se := range entries {
		branch := se.Entry.Branch
		if se.Entry.Mark != nil {
			branch = se.Entry.Mark.Branch
		} else if branch == "" {
			best := -1
			for _, f := range forks {
				if se.Seq > f.from && se.Seq < f.mark && f.from > best {
					best = f.from
					branch = f.branch
				}
			}
		}
		rows = append(rows, ReplayRow{Seq: se.Seq, Entry: se.Entry, Branch: branch})
	}
	branches := map[string]ForkBranch{}
	for _, se := range entries {
		m := se.Entry.Mark
		if m == nil {
			continue
		}
		st := branches[m.Branch]
		st.Branch = m.Branch
		switch m.Kind {
		case ForkKindStart:
			st.Task = m.Task
			st.Open = true
			st.Status = ""
			st.Steps = 0
		case ForkKindEnd:
			st.Status = m.Status
			st.Steps = m.Steps
			st.Open = false
		}
		branches[m.Branch] = st
	}
	return rows, branches
}
