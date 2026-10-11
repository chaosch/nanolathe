package fixture

import "math"

// DiagnosticSnapshot adds independently specified metadata and rigid-pose boundary cases.
// Its first two models still intentionally overlap; color checks reject depth ties.
func DiagnosticSnapshot(w *Workload) Snapshot {
	s := w.Sample(17)
	s.Previous[0][3] = .25
	s.Current[0][3] = .75
	s.Previous[0][7] = -2
	s.Current[0][7] = 4
	s.Previous[0][11] = 0
	s.Previous[1][15] = 0
	s.Current[2] = s.Previous[2]
	s.Current[2][12] += .01
	setBasis := func(at int, yawA, yawB float64) {
		a := rigid(yawA, 0, 0, 0, 0, 1)
		b := rigid(yawB, 0, 0, 0, 0, 1)
		a = Reflect(a)
		b = Reflect(b)
		for col := 0; col < 3; col++ {
			for row := 0; row < 3; row++ {
				s.Previous[at][col*4+row] = a[col*4+row]
				s.Current[at][col*4+row] = b[col*4+row]
			}
		}
	}
	setBasis(3, 179*math.Pi/180, -179*math.Pi/180)
	setBasis(4, .7, .700001)
	return s
}
