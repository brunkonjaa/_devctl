"""Run the real devctl repair CLI through a Windows ConPTY boundary.

This is a release-acceptance helper, not a repair implementation. It creates
fresh copies of a supplied clean failing fixture, starts the compiled Windows
executable in a pseudo-console, sends real console input, and reports the
process exit code plus the captured console stream.
"""

from __future__ import annotations

import argparse
import ctypes
import ctypes.wintypes as wintypes
import json
import os
from pathlib import Path
import queue
import shutil
import tempfile
import threading
import time


kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
HANDLE = wintypes.HANDLE
DWORD = wintypes.DWORD
LPVOID = wintypes.LPVOID


class Coord(ctypes.Structure):
    _fields_ = [("x", wintypes.SHORT), ("y", wintypes.SHORT)]


class StartupInfo(ctypes.Structure):
    _fields_ = [
        ("cb", DWORD),
        ("lpReserved", wintypes.LPWSTR),
        ("lpDesktop", wintypes.LPWSTR),
        ("lpTitle", wintypes.LPWSTR),
        ("dwX", DWORD),
        ("dwY", DWORD),
        ("dwXSize", DWORD),
        ("dwYSize", DWORD),
        ("dwXCountChars", DWORD),
        ("dwYCountChars", DWORD),
        ("dwFillAttribute", DWORD),
        ("dwFlags", DWORD),
        ("wShowWindow", wintypes.WORD),
        ("cbReserved2", wintypes.WORD),
        ("lpReserved2", ctypes.POINTER(wintypes.BYTE)),
        ("hStdInput", HANDLE),
        ("hStdOutput", HANDLE),
        ("hStdError", HANDLE),
    ]


class StartupInfoEx(ctypes.Structure):
    _fields_ = [("startup_info", StartupInfo), ("attribute_list", LPVOID)]


class ProcessInformation(ctypes.Structure):
    _fields_ = [
        ("process", HANDLE),
        ("thread", HANDLE),
        ("process_id", DWORD),
        ("thread_id", DWORD),
    ]


def bind_functions() -> None:
    kernel32.CreatePipe.argtypes = [
        ctypes.POINTER(HANDLE),
        ctypes.POINTER(HANDLE),
        ctypes.POINTER(StartupInfo),
        DWORD,
    ]
    kernel32.CreatePipe.restype = wintypes.BOOL
    kernel32.CreatePseudoConsole.argtypes = [
        Coord,
        HANDLE,
        HANDLE,
        DWORD,
        ctypes.POINTER(HANDLE),
    ]
    kernel32.CreatePseudoConsole.restype = wintypes.HRESULT
    kernel32.ClosePseudoConsole.argtypes = [HANDLE]
    kernel32.ClosePseudoConsole.restype = None
    kernel32.InitializeProcThreadAttributeList.argtypes = [
        LPVOID,
        DWORD,
        DWORD,
        ctypes.POINTER(ctypes.c_size_t),
    ]
    kernel32.InitializeProcThreadAttributeList.restype = wintypes.BOOL
    kernel32.UpdateProcThreadAttribute.argtypes = [
        LPVOID,
        DWORD,
        ctypes.c_size_t,
        LPVOID,
        ctypes.c_size_t,
        LPVOID,
        LPVOID,
    ]
    kernel32.UpdateProcThreadAttribute.restype = wintypes.BOOL
    kernel32.DeleteProcThreadAttributeList.argtypes = [LPVOID]
    kernel32.CreateProcessW.argtypes = [
        wintypes.LPCWSTR,
        wintypes.LPWSTR,
        LPVOID,
        LPVOID,
        wintypes.BOOL,
        DWORD,
        LPVOID,
        wintypes.LPCWSTR,
        ctypes.POINTER(StartupInfo),
        ctypes.POINTER(ProcessInformation),
    ]
    kernel32.CreateProcessW.restype = wintypes.BOOL
    kernel32.ReadFile.argtypes = [
        HANDLE,
        LPVOID,
        DWORD,
        ctypes.POINTER(DWORD),
        LPVOID,
    ]
    kernel32.ReadFile.restype = wintypes.BOOL
    kernel32.WriteFile.argtypes = [
        HANDLE,
        LPVOID,
        DWORD,
        ctypes.POINTER(DWORD),
        LPVOID,
    ]
    kernel32.WriteFile.restype = wintypes.BOOL
    kernel32.CloseHandle.argtypes = [HANDLE]
    kernel32.CloseHandle.restype = wintypes.BOOL
    kernel32.WaitForSingleObject.argtypes = [HANDLE, DWORD]
    kernel32.WaitForSingleObject.restype = DWORD
    kernel32.GetExitCodeProcess.argtypes = [HANDLE, ctypes.POINTER(DWORD)]
    kernel32.GetExitCodeProcess.restype = wintypes.BOOL
    kernel32.TerminateProcess.argtypes = [HANDLE, wintypes.UINT]
    kernel32.TerminateProcess.restype = wintypes.BOOL


EXTENDED_STARTUPINFO_PRESENT = 0x00080000
PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE = 0x00020016
WAIT_OBJECT_0 = 0
APPROVAL_PROMPT = "Apply this fix? [A] Apply [R] Reject [D] Details [C] Cancel"
INVALID_APPROVAL = "Please choose A, R, D, or C."
DETAILS_MARKER = "Project ID:"


def check(result: bool, name: str) -> None:
    if not result:
        raise OSError(ctypes.get_last_error(), name)


def run_case(executable: Path, source: Path, proposal: Path, case: str) -> dict:
    inputs = {
        "apply": "a\r",
        "reject": "r\r",
        "cancel": "c\r",
    }
    if case not in inputs and case not in {
        "invalid-reject",
        "details-reject",
        "eof",
        "ctrl-before-approval",
    }:
        raise ValueError(f"unsupported case: {case}")

    work = Path(tempfile.mkdtemp(prefix=f"devctl-stage7d-c-{case}-"))
    project = work / "project"
    shutil.copytree(source, project)
    command = (
        f'"{executable}" repair --json --proposal "{proposal}" '
        f'--allow calculator.go "{project}"'
    )

    input_read = HANDLE()
    input_write = HANDLE()
    output_read = HANDLE()
    output_write = HANDLE()
    check(
        kernel32.CreatePipe(
            ctypes.byref(input_read), ctypes.byref(input_write), None, 0
        ),
        "CreatePipe input",
    )
    check(
        kernel32.CreatePipe(
            ctypes.byref(output_read), ctypes.byref(output_write), None, 0
        ),
        "CreatePipe output",
    )
    pseudo_console = HANDLE()
    check(
        kernel32.CreatePseudoConsole(
            Coord(120, 40), input_read, output_write, 0, ctypes.byref(pseudo_console)
        )
        >= 0,
        "CreatePseudoConsole",
    )

    attribute_size = ctypes.c_size_t(0)
    kernel32.InitializeProcThreadAttributeList(
        None, 1, 0, ctypes.byref(attribute_size)
    )
    attribute_buffer = ctypes.create_string_buffer(attribute_size.value)
    check(
        kernel32.InitializeProcThreadAttributeList(
            attribute_buffer, 1, 0, ctypes.byref(attribute_size)
        ),
        "InitializeProcThreadAttributeList",
    )
    check(
        kernel32.UpdateProcThreadAttribute(
            attribute_buffer,
            0,
            PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
            pseudo_console,
            ctypes.sizeof(pseudo_console),
            None,
            None,
        ),
        "UpdateProcThreadAttribute",
    )

    startup_info = StartupInfoEx()
    startup_info.startup_info.cb = ctypes.sizeof(StartupInfoEx)
    startup_info.attribute_list = ctypes.cast(attribute_buffer, LPVOID)
    process_info = ProcessInformation()
    command_buffer = ctypes.create_unicode_buffer(command)
    check(
        kernel32.CreateProcessW(
            str(executable),
            command_buffer,
            None,
            None,
            False,
            EXTENDED_STARTUPINFO_PRESENT,
            None,
            None,
            ctypes.byref(startup_info.startup_info),
            ctypes.byref(process_info),
        ),
        "CreateProcessW",
    )
    kernel32.DeleteProcThreadAttributeList(attribute_buffer)
    # The handles supplied to CreatePseudoConsole belong to the host only
    # during child creation. Release them once the child owns the pseudo
    # console so channel lifetime and broken-pipe behavior remain accurate.
    kernel32.CloseHandle(input_read)
    kernel32.CloseHandle(output_write)
    kernel32.CloseHandle(process_info.thread)

    output_chunks: queue.Queue[bytes] = queue.Queue()
    stop_reader = threading.Event()

    def read_output() -> None:
        while not stop_reader.is_set():
            buffer = ctypes.create_string_buffer(8192)
            received = DWORD(0)
            if not kernel32.ReadFile(
                output_read, buffer, len(buffer), ctypes.byref(received), None
            ) or received.value == 0:
                return
            output_chunks.put(buffer.raw[: received.value])

    reader = threading.Thread(target=read_output, daemon=True)
    reader.start()
    output = bytearray()
    first_prompt_seen = False
    second_prompt_seen = False
    details_seen = False
    invalid_seen = False
    input_actions = []

    def write_input(payload: bytes, action: str) -> None:
        written = DWORD(0)
        check(
            kernel32.WriteFile(
                input_write, payload, len(payload), ctypes.byref(written), None
            ),
            f"WriteFile {action}",
        )
        input_actions.append({"action": action, "bytes": int(written.value)})

    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        while True:
            try:
                output.extend(output_chunks.get_nowait())
            except queue.Empty:
                break
        decoded = output.decode("utf-8", errors="replace")
        prompt_count = decoded.count(APPROVAL_PROMPT)
        if not first_prompt_seen and prompt_count >= 1:
            if case == "eof":
                kernel32.CloseHandle(input_write)
                input_actions.append({"action": "eof"})
            elif case == "ctrl-before-approval":
                write_input(b"\x03", "Ctrl+C")
            elif case in {"invalid-reject", "details-reject"}:
                write_input(
                    (b"x\r" if case == "invalid-reject" else b"d\r"),
                    "invalid approval" if case == "invalid-reject" else "details",
                )
            else:
                write_input(inputs[case].encode("utf-8"), case)
            first_prompt_seen = True
        if first_prompt_seen and not second_prompt_seen and prompt_count >= 2:
            if case == "invalid-reject" and INVALID_APPROVAL in decoded:
                invalid_seen = True
                write_input(b"r\r", "reject after invalid input")
                second_prompt_seen = True
            elif case == "details-reject" and DETAILS_MARKER in decoded:
                details_seen = True
                write_input(b"r\r", "reject after details")
                second_prompt_seen = True
        if kernel32.WaitForSingleObject(process_info.process, 50) == WAIT_OBJECT_0:
            break

    timed_out = kernel32.WaitForSingleObject(process_info.process, 0) != WAIT_OBJECT_0
    if timed_out:
        kernel32.TerminateProcess(process_info.process, 124)
        kernel32.WaitForSingleObject(process_info.process, 5000)
    time.sleep(0.25)
    while True:
        try:
            output.extend(output_chunks.get_nowait())
        except queue.Empty:
            break
    stop_reader.set()
    if case != "eof":
        kernel32.CloseHandle(input_write)
    exit_code = DWORD(0)
    kernel32.GetExitCodeProcess(process_info.process, ctypes.byref(exit_code))
    kernel32.CloseHandle(process_info.process)
    # The reader is intentionally a daemon: ReadFile is synchronous and
    # CancelSynchronousIo would require retaining a native thread handle.
    # The temporary host process owns these remaining handles and exits after
    # the child result has been collected.

    raw = output.decode("utf-8", errors="replace")
    json_lines = [
        line.strip()
        for line in raw.replace("\r", "").split("\n")
        if line.strip().startswith("{") and line.strip().endswith("}")
    ]
    structured = None
    if json_lines:
        try:
            structured = json.loads(json_lines[-1])
        except json.JSONDecodeError:
            pass
    return {
        "case": case,
        "exit_code": int(exit_code.value),
        "timed_out": timed_out,
        "approval_prompt_seen": first_prompt_seen,
        "approval_prompt_count": raw.count(APPROVAL_PROMPT),
        "waiting_message_seen": "Waiting for approval." in raw,
        "details_seen": details_seen,
        "invalid_message_seen": invalid_seen,
        "input_actions": input_actions,
        "structured": structured,
        "console": raw,
        "project": str(project),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--exe", required=True, type=Path)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--proposal", required=True, type=Path)
    parser.add_argument(
        "--case",
        required=True,
        choices=["apply", "reject", "cancel", "invalid-reject", "details-reject", "eof", "ctrl-before-approval"],
    )
    args = parser.parse_args()
    bind_functions()
    result = run_case(args.exe, args.source, args.proposal, args.case)
    print(json.dumps(result, indent=2))
    return 0 if not result["timed_out"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
