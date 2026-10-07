#!/usr/bin/env python3
"""Record an archived MinIO CLI binary or compare a candidate without accepting it.

All subprocesses use isolated HOME/cwd/configuration, separate output streams and
bounded process groups. Only controlled paths and build identities are normalized.
"""
import argparse
import difflib
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
CONTRACT_DIR = ROOT / "testdata" / "cli"


def load(path):
    return json.loads(Path(path).read_text())


def catalog_sha256(path):
    # Git can check out text as CRLF on Windows. Only that line-ending
    # conversion is ignored; all other catalog bytes remain review-sensitive.
    return hashlib.sha256(Path(path).read_bytes().replace(b"\r\n", b"\n")).hexdigest()


def environment(home):
    prefixes = ("OC_", "MC_", "OTTERIO_", "MINIO_", "AWS_", "COMP_")
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(prefixes)}
    env.update(HOME=str(home), USERPROFILE=str(home), XDG_CONFIG_HOME=str(home / ".config"),
               NO_COLOR="1", TERM="dumb", LC_ALL="C", TZ="UTC")
    return env


def normalize(value, directory):
    # Replace exact fixture roots; never strip whitespace, errors, flag defaults,
    # stream routing, or application-generated text.
    # HOME-derived defaults can be quoted by Go, including escaped Windows
    # separators. These remain exact controlled fixture paths, not arbitrary
    # path or output rewrites.
    for suffix in ("home/.otterio/certs", "home/.otterio", "home/.oc", "home"):
        native = str(directory.joinpath(*suffix.split("/")))
        for spelling in (native, json.dumps(native)[1:-1]):
            value = value.replace(spelling, "{sandbox}/" + suffix)
    for spelling in (str(directory), json.dumps(str(directory))[1:-1]):
        value = value.replace(spelling, "{sandbox}")
    return value


def bounded_run(command, env, cwd, timeout):
    process = subprocess.Popen(command, env=env, cwd=cwd, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               start_new_session=True)
    try:
        stdout, stderr = process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        if os.name == "nt":
            subprocess.run(["taskkill", "/F", "/T", "/PID", str(process.pid)],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
        else:
            os.killpg(process.pid, signal.SIGKILL)
        process.communicate()
        raise RuntimeError(f"CLI case timed out after {timeout}s: {command[1:]}")
    return process.returncode, stdout.decode("utf-8", "replace"), stderr.decode("utf-8", "replace")


def side_effects(directory, inputs):
    entries = []
    for path in sorted(directory.rglob("*")):
        name = str(path.relative_to(directory))
        if name in inputs:
            continue
        if path.is_symlink():
            entries.append({"path": name, "type": "symlink"})
        elif path.is_dir():
            entries.append({"path": name, "type": "directory"})
        else:
            data = path.read_bytes()
            try:
                data = normalize(data.decode("utf-8"), directory).encode("utf-8")
            except UnicodeDecodeError:
                pass
            entries.append({"path": name, "type": "file",
                            "sha256": hashlib.sha256(data).hexdigest()})
    return entries


def run_case(binary, project, case):
    with tempfile.TemporaryDirectory(prefix=f"{project}-cli-contract-",
                                     dir=os.environ.get("RUNNER_TEMP")) as temporary:
        directory = Path(temporary)
        home = directory / "home"
        home.mkdir()
        executable = directory / (project + (".exe" if os.name == "nt" else ""))
        if os.name == "nt":
            try:
                os.link(Path(binary).resolve(), executable)
            except OSError:
                shutil.copy2(Path(binary).resolve(), executable)
        else:
            executable.symlink_to(Path(binary).resolve())
        inputs = {executable.name, "home"}
        for name, contents in case.get("files", {}).items():
            path = directory / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(contents)
            inputs.add(name)
        env = environment(home)
        expand = lambda value: value.replace("{sandbox}", str(directory))
        env.update({name: expand(value) for name, value in case.get("env", {}).items()})
        argv = [expand(arg) for arg in case["argv"]]
        code, stdout, stderr = bounded_run([str(executable), *argv], env, directory,
                                          case.get("timeout", 15))
        return {"id": case["id"], "argv": case["argv"], "env": case.get("env", {}),
                "comparison": case.get("comparison", "exact"),
                "exit_code": code, "stdout": normalize(stdout, directory),
                "stderr": normalize(stderr, directory),
                "side_effects": side_effects(directory, inputs)}


def command_names(help_text):
    """Read only the commands section, preserving the framework's visible tree."""
    commands = []
    in_commands = False
    for line in help_text.splitlines():
        if line.strip() == "COMMANDS:":
            in_commands = True
            continue
        if in_commands and re.match(r"^[A-Z][A-Z _-]*:", line):
            break
        if in_commands:
            match = re.match(r"^\s{2}([a-zA-Z0-9][a-zA-Z0-9_-]*)(?:,\s*[a-zA-Z0-9_-]+)*\s{2,}", line)
            if match and match[1] not in ("help", "h"):
                commands.append(match[1])
    return commands


def discover_help(binary, project):
    pending = [()]
    seen = set()
    results = []
    while pending:
        path = pending.pop(0)
        if path in seen:
            continue
        seen.add(path)
        case = {"id": "help/" + ("/".join(path) or "root"), "argv": [*path, "--help"]}
        result = run_case(binary, project, case)
        if result["exit_code"] != 0:
            raise RuntimeError(f"visible command help failed: {path}: {result['stderr']}")
        results.append(result)
        for name in command_names(result["stdout"] + result["stderr"]):
            pending.append((*path, name))
        if len(seen) > 300:
            raise RuntimeError("visible command enumeration exceeded 300 nodes")
    return results


def parse_build_info(build_info):
    identity = {"dependencies": {}, "settings": {}, "main_path": None, "main_module": None,
                "replacements": []}
    for line in build_info.splitlines():
        fields = line.strip().split()
        if len(fields) >= 3 and fields[0] in ("dep", "mod"):
            identity["dependencies"][fields[1]] = fields[2]
            if fields[0] == "mod":
                identity["main_module"] = fields[1]
        elif len(fields) == 2 and fields[0] == "path":
            identity["main_path"] = fields[1]
        elif fields and fields[0] == "=>":
            identity["replacements"].append(fields[1:])
        elif len(fields) == 2 and fields[0] == "build" and "=" in fields[1]:
            name, value = fields[1].split("=", 1)
            identity["settings"][name] = value
    return identity


def binary_build_info(binary):
    return subprocess.run(["go", "version", "-m", str(Path(binary).resolve())],
                          text=True, capture_output=True, check=True).stdout


def binary_identity(binary):
    return parse_build_info(binary_build_info(binary))["dependencies"]


def validate_baseline_identity(expected, build_info, binary_sha256):
    """Validate a clean fixed checkout or the exact saved archive binary.

    Go may stamp the main module with a pseudo-version when .git is present;
    archive builds report (devel). Neither is an SDK dependency version.
    """
    source = expected["source"]
    actual = parse_build_info(build_info)
    archived = parse_build_info(source["build_info"])
    if actual["replacements"]:
        raise ValueError("--baseline-binary must not contain dependency replacements")
    if actual["dependencies"].get("github.com/minio/cli") != "v1.24.2":
        raise ValueError("--baseline-binary requires the archived minio/cli v1.24.2 binary")
    if (actual["main_path"], actual["main_module"]) != (archived["main_path"], archived["main_module"]):
        raise ValueError("--baseline-binary main package/module differs from the reviewed source identity")
    if expected["project"] == "oc" and actual["dependencies"].get("github.com/soulteary/otterio") != source["sdk_server_module"]:
        raise ValueError("--baseline-binary SDK dependency differs from the reviewed source identity")
    settings = actual["settings"]
    vcs_fields = {key: value for key, value in settings.items() if key.startswith("vcs")}
    if vcs_fields:
        if (settings.get("vcs") != "git" or settings.get("vcs.revision") != source["commit"]
                or settings.get("vcs.modified") != "false"):
            raise ValueError("--baseline-binary must come from the exact clean reviewed source commit")
    elif binary_sha256 != source["binary_sha256"]:
        raise ValueError("--baseline-binary without VCS identity must match the saved archive binary SHA256")


def capture(binary):
    manifest = load(CONTRACT_DIR / "cases.json")
    project = manifest["project"]
    return {"schema": 1, "project": project,
            "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            "build_info": subprocess.run(["go", "version", "-m", str(Path(binary).resolve())],
                                          text=True, capture_output=True, check=True).stdout,
            "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "manifest_sha256": catalog_sha256(CONTRACT_DIR / "cases.json"),
            "platform_family": "windows" if os.name == "nt" else "unix",
            "cases": discover_help(binary, project) +
                     [run_case(binary, project, case) for case in manifest["cases"]]}


def differences(expected, actual, approved_changes=None):
    old = {case["id"]: case for case in expected["cases"]}
    new = {case["id"]: case for case in actual["cases"]}
    if approved_changes and not approved_changes.keys() <= old.keys() & new.keys():
        raise ValueError("approved deltas cannot waive missing or added command cases")
    problems = []
    if expected.get("manifest_sha256") != actual.get("manifest_sha256"):
        problems.append("case catalog differs from the reviewed baseline; review the catalog change explicitly")
    if expected["platform_family"] != actual["platform_family"]:
        problems.append("baseline platform family differs; record and review that platform separately")
    for case_id in sorted(old.keys() | new.keys()):
        old_case = comparison_view(old.get(case_id))
        new_case = comparison_view(new.get(case_id))
        if approved_changes and case_id in approved_changes:
            change = dict(approved_changes[case_id])
            platform_fields = change.get("fields_after_by_platform", {}).get(actual["platform_family"])
            if platform_fields is not None:
                change["fields_after"] = platform_fields
            if not change.get("reason") or change.get("fields_before", {}).keys() != change.get("fields_after", {}).keys():
                raise ValueError(f"invalid approved delta: {case_id}")
            if not change["fields_after"].keys() <= {"exit_code", "stdout", "stderr", "side_effects"}:
                raise ValueError(f"approved delta cannot change invocation or contract policy: {case_id}")
            if old_case is None or new_case is None:
                raise ValueError(f"approved delta requires an existing exact case: {case_id}")
            if all(old_case.get(field) == value for field, value in change["fields_before"].items()):
                revised = dict(old_case)
                revised.update(change["fields_after"])
                if new_case == revised:
                    continue
            elif old_case != new_case:
                raise ValueError(f"approved delta does not match the archived case: {case_id}")
        if old_case != new_case:
            before = json.dumps(old_case, indent=2, ensure_ascii=False, sort_keys=True).splitlines(True)
            after = json.dumps(new_case, indent=2, ensure_ascii=False, sort_keys=True).splitlines(True)
            problems.append("".join(difflib.unified_diff(before, after, fromfile=f"baseline/{case_id}",
                                                        tofile=f"candidate/{case_id}")))
    return problems


def comparison_view(case):
    if case is None:
        return None
    case = dict(case)
    if case.get("comparison") == "completion-lines":
        # posener/complete already returns map order. Preserve every candidate,
        # including duplicates, while retaining raw stdout in the JSON report.
        case["stdout"] = sorted(case["stdout"].splitlines())
    elif case.get("comparison") == "legacy-panic" and case["stderr"].startswith("panic:"):
        # The archived defect's ASLR frames are forensic data, not stable CLI
        # output. Keep the complete raw stack in the report and its stable panic
        # line, exit code and side effects in the comparison.
        case["stderr"] = case["stderr"].splitlines()[0] + "\n"
    return case


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("record", "check"))
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", help="Optional candidate report; never updates the reviewed baseline")
    parser.add_argument("--source-ref", help="Required full commit SHA for initial archived baseline")
    parser.add_argument("--source-tree", help="Required archived source directory for initial baseline")
    parser.add_argument("--baseline-binary", help="Compare to an archived old binary built on the current CI platform")
    options = parser.parse_args()
    baseline_path = CONTRACT_DIR / "baseline.json"
    if options.mode == "record":
        if baseline_path.exists():
            parser.error("baseline already exists; changes require explicit review, not automatic regeneration")
        if not options.source_ref or not re.fullmatch(r"[0-9a-f]{40}", options.source_ref):
            parser.error("record requires the full archived source commit")
        if not options.source_tree:
            parser.error("record requires --source-tree for reproducible source identities")
        identity = parse_build_info(binary_build_info(options.binary))
        if identity["replacements"]:
            parser.error("baseline recording must not contain dependency replacements")
        dependencies = identity["dependencies"]
        if dependencies.get("github.com/minio/cli") != "v1.24.2":
            parser.error("baseline recording requires the archived minio/cli v1.24.2 binary")
        actual = capture(options.binary)
        source_tree = Path(options.source_tree)
        actual["source"] = {"commit": options.source_ref,
                            "go_mod_sha256": hashlib.sha256((source_tree / "go.mod").read_bytes()).hexdigest(),
                            "cli": {"module": "github.com/minio/cli", "version": "v1.24.2"},
                            "sdk_server_module": dependencies.get("github.com/soulteary/otterio"),
                            "server_started_by_contract": False,
                            "binary_sha256": hashlib.sha256(Path(options.binary).read_bytes()).hexdigest(),
                            "build_info": subprocess.run(["go", "version", "-m", str(Path(options.binary).resolve())],
                                                          text=True, capture_output=True, check=True).stdout,
                            "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                            "binary_dependencies": dependencies}
        baseline_path.write_text(json.dumps(actual, indent=2, ensure_ascii=False) + "\n")
        print(f"Recorded {len(actual['cases'])} isolated cases from archived {actual['project']} source")
        return 0
    expected = load(baseline_path)
    manifest_hash = catalog_sha256(CONTRACT_DIR / "cases.json")
    if manifest_hash != expected.get("manifest_sha256"):
        parser.error("case catalog differs from the reviewed baseline")
    if options.baseline_binary:
        try:
            validate_baseline_identity(expected, binary_build_info(options.baseline_binary),
                                       hashlib.sha256(Path(options.baseline_binary).read_bytes()).hexdigest())
        except ValueError as error:
            parser.error(str(error))
        expected = capture(options.baseline_binary)
    actual = capture(options.binary)
    if options.output:
        Path(options.output).write_text(json.dumps(actual, indent=2, ensure_ascii=False) + "\n")
    approved_path = CONTRACT_DIR / "approved-deltas.json"
    approved_changes = {}
    if approved_path.exists():
        approvals = load(approved_path)
        if approvals["source_commit"] != load(baseline_path)["source"]["commit"]:
            parser.error("approved deltas refer to a different archived source")
        approved_changes = approvals["cases"]
    problems = differences(expected, actual, approved_changes)
    if problems:
        print("\n".join(problems), file=sys.stderr)
        print(f"CLI contract differs in {len(problems)} case(s); baseline was not updated", file=sys.stderr)
        return 1
    print(f"CLI contract matched: {len(actual['cases'])} cases")
    return 0


if __name__ == "__main__":
    sys.exit(main())
