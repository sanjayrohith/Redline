#!/usr/bin/env python3
# probe_init_module.py — invokes the init_module(2) syscall directly via
# ctypes and prints its errno. Used to compare failure modes between the
# bare host (where the syscall exists but is permission-denied for an
# unprivileged caller: EPERM) and inside a gVisor sandbox (where the
# syscall is not implemented at all: ENOSYS) - a concrete, observable
# difference proving syscall interception is actually happening, not
# merely configured.
import ctypes
import os

libc = ctypes.CDLL("libc.so.6", use_errno=True)

SYS_INIT_MODULE = 175  # x86_64

ret = libc.syscall(SYS_INIT_MODULE, 0, 0, b"")
err = ctypes.get_errno()
print(f"ret={ret} errno={err} ({os.strerror(err)})")
