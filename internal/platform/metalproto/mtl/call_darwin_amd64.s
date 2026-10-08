//go:build darwin && amd64

#include "textflag.h"

// Call layout, in 8-byte words: fn, rdi rsi rdx rcx r8 r9, xmm0..xmm7,
// twelve stack words, then results rax, rdx and xmm0, xmm1. See abi_darwin_amd64.go.
#define C_FN 0
#define C_X 8
#define C_D 56
#define C_STACK 120
#define C_R 216
#define C_F 232

// floatTrampoline calls a C function with the System V register file and
// stack a Call describes. runtime.cgocall runs it on the system stack with
// DI = *Call. R12 keeps the call pointer across the call (callee-saved).
TEXT floatTrampoline<>(SB), NOSPLIT|NOFRAME, $0
	PUSHQ	BP
	MOVQ	SP, BP
	PUSHQ	R12
	PUSHQ	R13
	MOVQ	DI, R12
	SUBQ	$96, SP
	MOVQ	(C_STACK+0)(R12), AX
	MOVQ	AX, 0(SP)
	MOVQ	(C_STACK+8)(R12), AX
	MOVQ	AX, 8(SP)
	MOVQ	(C_STACK+16)(R12), AX
	MOVQ	AX, 16(SP)
	MOVQ	(C_STACK+24)(R12), AX
	MOVQ	AX, 24(SP)
	MOVQ	(C_STACK+32)(R12), AX
	MOVQ	AX, 32(SP)
	MOVQ	(C_STACK+40)(R12), AX
	MOVQ	AX, 40(SP)
	MOVQ	(C_STACK+48)(R12), AX
	MOVQ	AX, 48(SP)
	MOVQ	(C_STACK+56)(R12), AX
	MOVQ	AX, 56(SP)
	MOVQ	(C_STACK+64)(R12), AX
	MOVQ	AX, 64(SP)
	MOVQ	(C_STACK+72)(R12), AX
	MOVQ	AX, 72(SP)
	MOVQ	(C_STACK+80)(R12), AX
	MOVQ	AX, 80(SP)
	MOVQ	(C_STACK+88)(R12), AX
	MOVQ	AX, 88(SP)
	MOVSD	(C_D+0)(R12), X0
	MOVSD	(C_D+8)(R12), X1
	MOVSD	(C_D+16)(R12), X2
	MOVSD	(C_D+24)(R12), X3
	MOVSD	(C_D+32)(R12), X4
	MOVSD	(C_D+40)(R12), X5
	MOVSD	(C_D+48)(R12), X6
	MOVSD	(C_D+56)(R12), X7
	MOVQ	(C_X+0)(R12), DI
	MOVQ	(C_X+8)(R12), SI
	MOVQ	(C_X+16)(R12), DX
	MOVQ	(C_X+24)(R12), CX
	MOVQ	(C_X+32)(R12), R8
	MOVQ	(C_X+40)(R12), R9
	MOVQ	C_FN(R12), R11
	MOVL	$8, AX	// vector registers used, for variadic callees
	CALL	R11
	MOVQ	AX, (C_R+0)(R12)
	MOVQ	DX, (C_R+8)(R12)
	MOVSD	X0, (C_F+0)(R12)
	MOVSD	X1, (C_F+8)(R12)
	LEAQ	-16(BP), SP
	POPQ	R13
	POPQ	R12
	POPQ	BP
	RET

GLOBL ·floatTrampolineABI0(SB), NOPTR|RODATA, $8
DATA ·floatTrampolineABI0(SB)/8, $floatTrampoline<>(SB)
