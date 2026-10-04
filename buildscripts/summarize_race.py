"""Summarize a go test -json stream; the go test exit code remains the gate."""
import argparse
from collections import Counter
import json
from pathlib import Path


def summarize(lines):
    packages, tests = {}, {}
    started_packages, finished_packages = set(), set()
    for number, line in enumerate(lines, 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as error:
            raise ValueError(f"invalid JSON on line {number}") from error
        action, package, test = (event.get(key) for key in ("Action", "Package", "Test"))
        if not package:
            continue
        if action == "start" and not test:
            started_packages.add(package)
        if action not in ("pass", "fail", "skip"):
            continue
        elapsed = float(event.get("Elapsed", 0))
        if not test:
            packages[package] = (action, elapsed)
            finished_packages.add(package)
        elif "/" not in test:
            # Parent elapsed already includes subtests; do not double count.
            tests[(package, test)] = (action, elapsed)

    output = ["## Race test diagnostics", ""]
    for label, results in (("Packages", packages), ("Top-level tests", tests)):
        counts = Counter(result[0] for result in results.values())
        output.append(f"{label}: {counts['pass']} passed, {counts['fail']} failed, {counts['skip']} skipped.")
    unfinished = started_packages - finished_packages
    if unfinished:
        output.append(f"Incomplete package results: {len(unfinished)} (failure, timeout or cancellation may have truncated the log).")
    if not packages:
        output.append("No completed package results; this is not evidence of a passing suite.")
    output += ["", "Elapsed values exclude compilation before each test binary starts and are not additive wall-clock time.", ""]
    for title, results in (("Slowest packages", packages), ("Slowest top-level tests", tests)):
        output += [f"### {title}", "", "| Name | Result | Seconds |", "| --- | --- | ---: |"]
        ordered = sorted(results.items(), key=lambda item: (-item[1][1], str(item[0])))[:15]
        for name, (action, elapsed) in ordered:
            label = " / ".join(name) if isinstance(name, tuple) else name
            label = label.replace("|", "\\|").replace("`", "'").replace("\n", " ")
            output.append(f"| `{label}` | {action} | {elapsed:.3f} |")
        output.append("")
    return "\n".join(output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log", type=Path)
    args = parser.parse_args()
    if not args.log.is_file():
        print("## Race test diagnostics\n\nNo event log was produced; inspect the failed setup/test step.")
        return
    with args.log.open(encoding="utf-8") as stream:
        print(summarize(stream))


if __name__ == "__main__":
    main()
