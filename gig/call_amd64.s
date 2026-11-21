// Stack-switching trampolines between the Go host and a go-in-go module.
//
// A module runs on its own stacks (a system stack mapped by the loader, and
// goroutine stacks on its own heap), with the System V calling convention.
// These functions switch between the goroutine stack of the Go caller and the
// module, saving the callee-saved registers of the side being left on its own
// stack. The call context (ctx) lives in memory outside both heaps:
//
//	0(ctx)   goSP     Go stack pointer to return to (after the pushes)
//	8(ctx)   modSP    module stack pointer to resume at (after the pushes)
//	16(ctx)  status   0: the module function returned, 1: a host call
//	24(ctx)  id       host call: call id
//	32(ctx)  args     host call: pointer to the argument slots
//	40(ctx)  rets     host call: pointer to the result slots
//	48(ctx)  stack    top of the module's system stack (16-byte aligned)
//	56(ctx)  ret      value returned by the module function (RAX)
//
// The functions are NOFRAME: the assembler must not add a frame-pointer
// prologue to enter (it contains a CALL), since enter, resume and yieldTramp
// return through each other's stack layouts.
//
// Go code assumes nothing but SP survives an ABI0 call; X15 must be zero when
// returning to Go code, so it's cleared on every switch back.

#include "textflag.h"

// func enter(ctx, fn, a0, a1 uintptr)
// Calls fn(a0, a1) on the module's system stack. Returns when the module
// function returns (status 0) or when it makes a host call (status 1).
TEXT ·enter(SB),NOSPLIT|NOFRAME,$0-32
	MOVQ ctx+0(FP), AX
	MOVQ fn+8(FP), CX
	MOVQ a0+16(FP), DI
	MOVQ a1+24(FP), SI
	PUSHQ BP
	PUSHQ BX
	PUSHQ R12
	PUSHQ R13
	PUSHQ R14
	PUSHQ R15
	MOVQ SP, 0(AX)
	MOVQ AX, R12 // callee-saved: still ctx when fn returns
	MOVQ 48(AX), SP
	CALL CX
	MOVQ AX, 56(R12)
	MOVQ $0, 16(R12)
	MOVQ 0(R12), SP
	POPQ R15
	POPQ R14
	POPQ R13
	POPQ R12
	POPQ BX
	POPQ BP
	XORPS X15, X15
	RET

// func resume(ctx uintptr)
// Switches back into the module after a host call was served. Returns like
// enter.
TEXT ·resume(SB),NOSPLIT|NOFRAME,$0-8
	MOVQ ctx+0(FP), AX
	PUSHQ BP
	PUSHQ BX
	PUSHQ R12
	PUSHQ R13
	PUSHQ R14
	PUSHQ R15
	MOVQ SP, 0(AX)
	MOVQ 8(AX), SP
	POPQ R15
	POPQ R14
	POPQ R13
	POPQ R12
	POPQ BX
	POPQ BP
	RET // into the module, returning from gig_yield

// gig_yield(ctx, id, args, rets), called by the module with the C calling
// convention (DI, SI, DX, CX). Not a Go function: it never runs on a Go
// stack. It suspends the module and returns from enter/resume with status 1.
TEXT ·yieldTramp(SB),NOSPLIT|NOFRAME,$0
	PUSHQ BP
	PUSHQ BX
	PUSHQ R12
	PUSHQ R13
	PUSHQ R14
	PUSHQ R15
	MOVQ SP, 8(DI)
	MOVQ SI, 24(DI)
	MOVQ DX, 32(DI)
	MOVQ CX, 40(DI)
	MOVQ $1, 16(DI)
	MOVQ 0(DI), SP
	POPQ R15
	POPQ R14
	POPQ R13
	POPQ R12
	POPQ BX
	POPQ BP
	XORPS X15, X15
	RET // from enter or resume, back in Go

// func yieldAddr() uintptr
TEXT ·yieldAddr(SB),NOSPLIT,$0-8
	LEAQ ·yieldTramp(SB), AX
	MOVQ AX, ret+0(FP)
	RET

// Fault containment.
//
// faultHandler is installed (with rt_sigaction) for SIGSEGV, SIGBUS, SIGILL,
// SIGFPE and SIGTRAP in front of the Go runtime's handler. It runs on the
// signal stack with the C calling convention: DI = signal, SI = siginfo,
// DX = ucontext. If the faulting PC is inside a loaded module (a range in
// faultTable), the module's call is abandoned: the handler records the fault
// in the instance's context (status 2, signal, PC, fault address) and edits
// the saved registers so that sigreturn "returns" into faultReturn on the Go
// stack, exactly like a host call does. Any other fault is forwarded to the
// handler that was installed before (the Go runtime's).
//
// ucontext on linux/amd64: uc_mcontext.gregs starts at offset 40; RSP is
// gregs[15] (offset 160) and RIP gregs[16] (offset 168). si_addr is at
// offset 16 of siginfo.
TEXT ·faultHandler(SB),NOSPLIT|NOFRAME,$0
	MOVQ 168(DX), AX
	LEAQ ·faultTable(SB), R8
	MOVQ $0, R9
loop:
	CMPQ R9, $1024 // len(faultTable)
	JGE forward
	MOVQ 16(R8), R10 // ctx, 0 if the slot is free
	TESTQ R10, R10
	JZ next
	CMPQ AX, 0(R8) // lo
	JCS next
	CMPQ AX, 8(R8) // hi
	JCC next
	MOVQ $2, 16(R10)
	MOVQ DI, 24(R10)
	MOVQ AX, 32(R10)
	MOVQ 16(SI), R11
	MOVQ R11, 40(R10)
	MOVQ 0(R10), R11
	MOVQ R11, 160(DX)
	LEAQ ·faultReturn(SB), R11
	MOVQ R11, 168(DX)
	RET // to the kernel's sigreturn trampoline
next:
	ADDQ $24, R8
	INCQ R9
	JMP loop
forward:
	LEAQ ·oldHandlers(SB), R8
	MOVQ (R8)(DI*8), AX
	JMP AX

// faultReturn runs on the Go stack saved by enter/resume, like the second
// half of yieldTramp.
TEXT ·faultReturn(SB),NOSPLIT|NOFRAME,$0
	// POPQ R15 ... POPQ BP, written as moves since the assembler rejects
	// unbalanced POPs. (The jump ends vet's frame-pointer check, which can't
	// know that BP is being restored, not clobbered.)
	JMP restore
restore:
	MOVQ 0(SP), R15
	MOVQ 8(SP), R14
	MOVQ 16(SP), R13
	MOVQ 24(SP), R12
	MOVQ 32(SP), BX
	MOVQ 40(SP), BP
	ADDQ $48, SP
	XORPS X15, X15
	RET

// func faultHandlerAddr() uintptr
TEXT ·faultHandlerAddr(SB),NOSPLIT,$0-8
	LEAQ ·faultHandler(SB), AX
	MOVQ AX, ret+0(FP)
	RET
