//go:build darwin && arm64

#include "textflag.h"

// Call layout, in 8-byte words: fn, x0..x7, d0..d7, four stack words, x8,
// then results r0, r1 and d0..d3. See call_darwin.go.
#define C_FN 0
#define C_X 8
#define C_D 72
#define C_STACK 136
#define C_X8 168
#define C_R 176
#define C_F 192

// floatTrampoline calls a C function with the AAPCS64 register file a Call
// describes. runtime.cgocall runs it on the system stack with R0 = *Call.
// It keeps its own frame: the call pointer and return address are saved
// above the four outgoing stack argument words.
TEXT floatTrampoline<>(SB), NOSPLIT|NOFRAME, $0
	SUB	$48, RSP
	MOVD	R30, 40(RSP)
	MOVD	R0, 32(RSP)
	MOVD	R0, R9
	MOVD	(C_STACK+0)(R9), R10
	MOVD	R10, 0(RSP)
	MOVD	(C_STACK+8)(R9), R10
	MOVD	R10, 8(RSP)
	MOVD	(C_STACK+16)(R9), R10
	MOVD	R10, 16(RSP)
	MOVD	(C_STACK+24)(R9), R10
	MOVD	R10, 24(RSP)
	FMOVD	(C_D+0)(R9), F0
	FMOVD	(C_D+8)(R9), F1
	FMOVD	(C_D+16)(R9), F2
	FMOVD	(C_D+24)(R9), F3
	FMOVD	(C_D+32)(R9), F4
	FMOVD	(C_D+40)(R9), F5
	FMOVD	(C_D+48)(R9), F6
	FMOVD	(C_D+56)(R9), F7
	MOVD	(C_X+0)(R9), R0
	MOVD	(C_X+8)(R9), R1
	MOVD	(C_X+16)(R9), R2
	MOVD	(C_X+24)(R9), R3
	MOVD	(C_X+32)(R9), R4
	MOVD	(C_X+40)(R9), R5
	MOVD	(C_X+48)(R9), R6
	MOVD	(C_X+56)(R9), R7
	MOVD	C_X8(R9), R8
	MOVD	C_FN(R9), R10
	BL	(R10)
	MOVD	32(RSP), R9
	MOVD	R0, (C_R+0)(R9)
	MOVD	R1, (C_R+8)(R9)
	FMOVD	F0, (C_F+0)(R9)
	FMOVD	F1, (C_F+8)(R9)
	FMOVD	F2, (C_F+16)(R9)
	FMOVD	F3, (C_F+24)(R9)
	MOVD	40(RSP), R30
	ADD	$48, RSP
	RET

GLOBL ·floatTrampolineABI0(SB), NOPTR|RODATA, $8
DATA ·floatTrampolineABI0(SB)/8, $floatTrampoline<>(SB)
