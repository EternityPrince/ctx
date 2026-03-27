import ast


def build_function_symbol(info, module_name, node):
    qname = dotted(module_name, node.name)
    return {
        "SymbolKey": f"func|{module_name}|{node.name}",
        "QName": qname,
        "PackageImportPath": info["package_path"],
        "FilePath": info["rel_path"],
        "Name": node.name,
        "Kind": "func",
        "Receiver": "",
        "Signature": render_function_signature(node),
        "Doc": ast.get_docstring(node, clean=True) or "",
        "Line": node.lineno,
        "Column": node.col_offset + 1,
        "Exported": not node.name.startswith("_"),
        "IsTest": False,
    }


def build_class_symbol(info, module_name, node):
    qname = dotted(module_name, node.name)
    return {
        "SymbolKey": f"class|{module_name}|{node.name}",
        "QName": qname,
        "PackageImportPath": info["package_path"],
        "FilePath": info["rel_path"],
        "Name": node.name,
        "Kind": "class",
        "Receiver": "",
        "Signature": render_class_signature(node),
        "Doc": ast.get_docstring(node, clean=True) or "",
        "Line": node.lineno,
        "Column": node.col_offset + 1,
        "Exported": not node.name.startswith("_"),
        "IsTest": False,
    }


def build_method_symbol(info, module_name, class_node, node):
    qname = dotted(module_name, class_node.name, node.name)
    return {
        "SymbolKey": f"method|{module_name}|{class_node.name}|{node.name}",
        "QName": qname,
        "PackageImportPath": info["package_path"],
        "FilePath": info["rel_path"],
        "Name": node.name,
        "Kind": "method",
        "Receiver": class_node.name,
        "Signature": render_function_signature(node),
        "Doc": ast.get_docstring(node, clean=True) or "",
        "Line": node.lineno,
        "Column": node.col_offset + 1,
        "Exported": not node.name.startswith("_"),
        "IsTest": False,
    }


def render_function_signature(node):
    prefix = "async " if isinstance(node, ast.AsyncFunctionDef) else ""
    args = render_args(node.args)
    returns = ""
    if node.returns is not None:
        returns = f" -> {safe_unparse(node.returns)}"
    return f"{prefix}def {node.name}({args}){returns}"


def render_class_signature(node):
    bases = [safe_unparse(base) for base in node.bases]
    if bases:
        return f"class {node.name}({', '.join(bases)})"
    return f"class {node.name}"


def render_args(args):
    parts = []
    positional = list(args.posonlyargs) + list(args.args)
    defaults = [None] * (len(positional) - len(args.defaults)) + list(args.defaults)

    for index, arg in enumerate(positional):
        value = render_arg(arg)
        default = defaults[index]
        if default is not None:
            value += f"={safe_unparse(default)}"
        parts.append(value)
        if args.posonlyargs and index == len(args.posonlyargs) - 1:
            parts.append("/")

    if args.vararg:
        parts.append("*" + render_arg(args.vararg))
    elif args.kwonlyargs:
        parts.append("*")

    for arg, default in zip(args.kwonlyargs, args.kw_defaults):
        value = render_arg(arg)
        if default is not None:
            value += f"={safe_unparse(default)}"
        parts.append(value)

    if args.kwarg:
        parts.append("**" + render_arg(args.kwarg))
    return ", ".join(parts)


def render_arg(arg):
    value = arg.arg
    if arg.annotation is not None:
        value += f": {safe_unparse(arg.annotation)}"
    return value


def safe_unparse(node):
    try:
        return ast.unparse(node)
    except Exception:
        return ""


def package_basename(package_path, module_name):
    if package_path:
        return package_path.split(".")[-1]
    if module_name:
        return module_name.split(".")[-1]
    return "root"


def reference_kind(kind, meta="", annotation=False):
    is_type = kind == "class"

    if meta in ("type_checking_import", "type_checking_reexport"):
        return "type_checking_type" if is_type else "type_checking"
    if meta == "re_export":
        return "reexport_type" if is_type else "reexport"
    if annotation:
        return "annotation_type" if is_type else "annotation"
    if is_type:
        return "type"
    return "value"


def dotted(*parts):
    values = [part for part in parts if part]
    return ".".join(values)
