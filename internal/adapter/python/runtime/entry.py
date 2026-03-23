import json
import sys

from analyzer import PythonAnalyzer


def main():
    payload = json.load(sys.stdin)
    json.dump(PythonAnalyzer(payload).run(), sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    main()
