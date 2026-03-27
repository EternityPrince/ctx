def python_package_import_path(project_name, rel_path, source_roots=None):
    parts = python_import_parts(rel_path, source_roots)
    if not parts:
        return project_name
    if len(parts) == 1:
        return parts[0]
    return ".".join(parts[:-1])


def python_module_import_path(project_name, rel_path, source_roots=None):
    parts = python_import_parts(rel_path, source_roots)
    if not parts:
        return project_name
    return ".".join(parts)


def python_import_parts(rel_path, source_roots=None):
    rel_path = rel_path.replace("\\", "/")
    rel_path = trim_python_source_root(rel_path, source_roots or ["src"])
    if rel_path.endswith(".py"):
        rel_path = rel_path[:-3]
    parts = [part for part in rel_path.split("/") if part and part != "."]
    if parts and parts[-1] == "__init__":
        parts = parts[:-1]
    return parts


def trim_python_source_root(rel_path, source_roots):
    best = ""
    for root in sorted(set(source_roots or []), key=lambda value: len(value), reverse=True):
        root = (root or "").replace("\\", "/").strip()
        root = root[2:] if root.startswith("./") else root
        root = root.rstrip("/")
        if not root:
            continue
        if rel_path == root or rel_path.startswith(root + "/"):
            best = root
            break
    if not best:
        return rel_path
    if rel_path == best:
        return ""
    return rel_path[len(best) + 1 :]


def normalize_module_name(name, module_to_package, module_prefixes=None):
    if not name:
        return ""
    if name in module_to_package:
        return name
    if name in (module_prefixes or set()):
        return name
    return ""


def resolve_import_from_module(current_package, module, level):
    if level <= 0:
        return module
    parts = current_package.split(".") if current_package else []
    trim = max(level - 1, 0)
    if trim >= len(parts):
        parts = []
    else:
        parts = parts[: len(parts) - trim]
    if module:
        parts.extend(part for part in module.split(".") if part)
    return ".".join(parts)


def normalize_name(value):
    value = value or ""
    return value.replace("_", "").replace("-", "").lower()


def is_python_test_name(name):
    return name.startswith("test")


def is_python_test_class(node):
    if node.name.startswith("Test"):
        return True
    for base in node.bases:
        rendered = safe_render(base)
        if rendered.endswith("TestCase") or rendered == "TestCase":
            return True
    return False


def trim_test_name(name):
    if name.startswith("test_"):
        return name[5:]
    if name.startswith("test"):
        return name[4:]
    return ""


def trim_test_class_name(name):
    if name.startswith("Test"):
        return name[4:]
    return name


def safe_render(node):
    try:
        import ast

        return ast.unparse(node)
    except Exception:
        return ""
