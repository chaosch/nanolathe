package fixture

import (
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"math"
)

// Blend is the independent double-precision diagnostic oracle. Production state is never read.
// It returns a rigid basis; world-Z reflection and metadata are outside quaternion extraction.
func Blend(a, b meshscene.Mat4, alpha float32) meshscene.Mat4 {
	if a[15] < .5 || b[15] < .5 {
		return meshscene.Mat4{}
	}
	t := float64(max(float32(0), min(float32(1), alpha)))
	m := a
	same := true
	for c := 0; c < 3; c++ {
		for r := 0; r < 3; r++ {
			same = same && a[c*4+r] == b[c*4+r]
		}
	}
	if t >= 1 {
		m = b
	} else if t > 0 {
		if !same {
			qa, qb := quaternion(a), quaternion(b)
			dot := 0.0
			for i := range qa {
				dot += qa[i] * qb[i]
			}
			if dot < 0 {
				for i := range qb {
					qb[i] = -qb[i]
				}
				dot = -dot
			}
			dot = max(0, min(1, dot))
			var q [4]float64
			if dot > .9995 {
				for i := range q {
					q[i] = qa[i]*(1-t) + qb[i]*t
				}
			} else {
				theta := math.Acos(dot)
				for i := range q {
					q[i] = (math.Sin((1-t)*theta)*qa[i] + math.Sin(t*theta)*qb[i]) / math.Sin(theta)
				}
			}
			length := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
			for i := range q {
				q[i] /= length
			}
			m = rotation(q)
		}
		for i := 12; i < 15; i++ {
			if a[i] == b[i] {
				m[i] = b[i]
			} else {
				m[i] = float32(float64(a[i])*(1-t) + float64(b[i])*t)
			}
		}
	}
	m[3] = b[3]
	m[15] = b[15]
	m[7] = float32(float64(a[7])*(1-t) + float64(b[7])*t)
	if a[7] == b[7] {
		m[7] = b[7]
	}
	m[11] = 0
	if a[11] > .5 && b[11] > .5 {
		m[11] = 1
	}
	return m
}
func quaternion(m meshscene.Mat4) [4]float64 {
	x := [3]float64{float64(m[0]), float64(m[1]), -float64(m[2])}
	y := [3]float64{float64(m[4]), float64(m[5]), -float64(m[6])}
	normalize := func(v [3]float64) [3]float64 {
		l := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
		for i := range v {
			v[i] /= l
		}
		return v
	}
	x = normalize(x)
	d := x[0]*y[0] + x[1]*y[1] + x[2]*y[2]
	for i := range y {
		y[i] -= d * x[i]
	}
	y = normalize(y)
	z := [3]float64{x[1]*y[2] - x[2]*y[1], x[2]*y[0] - x[0]*y[2], x[0]*y[1] - x[1]*y[0]}
	var q [4]float64
	trace := x[0] + y[1] + z[2]
	switch {
	case trace > 0:
		s := 2 * math.Sqrt(trace+1)
		q = [4]float64{(y[2] - z[1]) / s, (z[0] - x[2]) / s, (x[1] - y[0]) / s, s / 4}
	case x[0] > y[1] && x[0] > z[2]:
		s := 2 * math.Sqrt(1+x[0]-y[1]-z[2])
		q = [4]float64{s / 4, (y[0] + x[1]) / s, (z[0] + x[2]) / s, (y[2] - z[1]) / s}
	case y[1] > z[2]:
		s := 2 * math.Sqrt(1+y[1]-x[0]-z[2])
		q = [4]float64{(y[0] + x[1]) / s, s / 4, (z[1] + y[2]) / s, (z[0] - x[2]) / s}
	default:
		s := 2 * math.Sqrt(1+z[2]-x[0]-y[1])
		q = [4]float64{(z[0] + x[2]) / s, (z[1] + y[2]) / s, s / 4, (x[1] - y[0]) / s}
	}
	l := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	for i := range q {
		q[i] /= l
	}
	return q
}
func rotation(q [4]float64) meshscene.Mat4 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	m := meshscene.Mat4{float32(1 - 2*(y*y+z*z)), float32(2 * (x*y + z*w)), float32(-2 * (x*z - y*w)), 0, float32(2 * (x*y - z*w)), float32(1 - 2*(x*x+z*z)), float32(-2 * (y*z + x*w)), 0, float32(2 * (x*z + y*w)), float32(2 * (y*z - x*w)), float32(-(1 - 2*(x*x+y*y))), 0, 0, 0, 0, 1}
	return m
}
func Point(m meshscene.Mat4, p [3]float32) [3]float32 {
	var out [3]float32
	for r := 0; r < 3; r++ {
		out[r] = m[r]*p[0] + m[4+r]*p[1] + m[8+r]*p[2] + m[12+r]
	}
	return out
}
