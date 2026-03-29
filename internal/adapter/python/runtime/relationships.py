import ast

from modules import normalize_module_name, resolve_import_from_module
from symbols import reference_kind


def collect_local_names(node):
    names = {
        arg.arg
        for arg in list(node.args.posonlyargs) + list(node.args.args) + list(node.args.kwonlyargs)
    }
    if node.args.vararg:
        names.add(node.args.vararg.arg)
    if node.args.kwarg:
        names.add(node.args.kwarg.arg)

    class Collector(ast.NodeVisitor):
        def visit_FunctionDef(self, inner):
            return

        def visit_AsyncFunctionDef(self, inner):
            return

        def visit_ClassDef(self, inner):
            return

        def visit_Name(self, inner):
            if isinstance(inner.ctx, ast.Store):
                names.add(inner.id)

        def visit_Import(self, inner):
            for alias in inner.names:
                names.add(alias.asname or alias.name.split(".")[0])

        def visit_ImportFrom(self, inner):
            for alias in inner.names:
                if alias.name != "*":
                    names.add(alias.asname or alias.name)

    collector = Collector()
    for statement in node.body:
        collector.visit(statement)
    return names


def binding_symbol(binding):
    if not binding:
        return None
    kind, value, _ = binding_parts(binding)
    if kind in ("symbol", "instance"):
        return value
    return None


def binding_parts(binding):
    if not binding:
        return None, None, ""
    kind = binding[0] if len(binding) > 0 else None
    value = binding[1] if len(binding) > 1 else None
    meta = binding[2] if len(binding) > 2 else ""
    return kind, value, meta


def normalize_annotation_expr(annotation):
    if annotation is None:
        return None
    if isinstance(annotation, ast.Constant) and isinstance(annotation.value, str):
        try:
            parsed = ast.parse(annotation.value, mode="eval")
        except SyntaxError:
            return None
        return parsed.body
    return annotation


class ScopeState:
    def __init__(
        self,
        analyzer,
        rel_path,
        package_path,
        module_name,
        owner,
        class_symbol,
        module_aliases,
        module_alias_meta,
        symbol_aliases,
        symbol_alias_meta,
        module_loader_aliases,
        import_call_aliases,
        local_names,
    ):
        self.analyzer = analyzer
        self.rel_path = rel_path
        self.package_path = package_path
        self.module_name = module_name
        self.owner = owner
        self.class_symbol = class_symbol
        self.module_aliases = dict(module_aliases)
        self.module_alias_meta = dict(module_alias_meta)
        self.symbol_aliases = dict(symbol_aliases)
        self.symbol_alias_meta = dict(symbol_alias_meta)
        self.module_loader_aliases = set(module_loader_aliases)
        self.import_call_aliases = set(import_call_aliases)
        self.local_names = set(local_names)
        self.local_bindings = {}
        self.param_names = set()
        self.in_annotation = False
        self.in_type_checking = False

        class_key = class_symbol["SymbolKey"] if class_symbol else ""
        self.attribute_bindings = dict(analyzer.class_attribute_bindings.get(class_key, {}))
        if class_symbol:
            self.bind_name("self", ("instance", class_symbol))
            self.bind_name("cls", ("symbol", class_symbol))

    def seed_function_args(self, args):
        for arg in list(args.posonlyargs) + list(args.args) + list(args.kwonlyargs):
            self.bind_name(arg.arg, self.binding_from_annotation(arg.annotation))
            if arg.arg not in ("self", "cls"):
                self.param_names.add(arg.arg)
        if args.vararg:
            self.bind_name(args.vararg.arg, None)
            self.param_names.add(args.vararg.arg)
        if args.kwarg:
            self.bind_name(args.kwarg.arg, None)
            self.param_names.add(args.kwarg.arg)

    def bind_name(self, name, binding):
        self.local_names.add(name)
        if binding:
            self.local_bindings[name] = binding
        else:
            self.local_bindings.pop(name, None)

    def bind_attribute(self, name, binding):
        if not self.class_symbol:
            return

        class_key = self.class_symbol["SymbolKey"]
        if binding:
            self.attribute_bindings[name] = binding
            self.analyzer.class_attribute_bindings.setdefault(class_key, {})[name] = binding
        else:
            self.attribute_bindings.pop(name, None)
            if class_key in self.analyzer.class_attribute_bindings:
                self.analyzer.class_attribute_bindings[class_key].pop(name, None)

    def bind_target(self, target, binding):
        if isinstance(target, ast.Name):
            self.bind_name(target.id, binding)
            return

        if isinstance(target, ast.Attribute) and isinstance(target.value, ast.Name) and target.value.id in ("self", "cls"):
            self.bind_attribute(target.attr, binding)
            return

        if isinstance(target, (ast.Tuple, ast.List)):
            for element in target.elts:
                self.bind_target(element, None)

    def binding_from_annotation(self, annotation):
        annotation = normalize_annotation_expr(annotation)
        if annotation is None:
            return None
        kind, value, meta = self.resolve_expr_meta(annotation, ignore_locals=True)
        if kind == "symbol" and value and value["Kind"] == "class":
            return ("instance", value, meta)
        if kind == "instance" and value:
            return ("instance", value, meta)
        return None

    def binding_from_value(self, value, annotation=None):
        if annotation is not None:
            binding = self.binding_from_annotation(annotation)
            if binding:
                return binding
        if value is None:
            return None
        kind, target, meta = self.resolve_expr_meta(value)
        if kind in ("module", "symbol", "instance"):
            return (kind, target, meta)
        return None

    def resolve_expr(self, expr, ignore_locals=False):
        kind, value, _ = self.resolve_expr_meta(expr, ignore_locals=ignore_locals)
        return kind, value

    def resolve_expr_meta(self, expr, ignore_locals=False):
        if isinstance(expr, ast.Name):
            if not ignore_locals and expr.id in self.local_bindings:
                return binding_parts(self.local_bindings[expr.id])
            if expr.id in self.symbol_aliases:
                return (
                    "symbol",
                    self.analyzer.symbols_by_key.get(self.symbol_aliases[expr.id]),
                    self.symbol_alias_meta.get(expr.id, ""),
                )
            if expr.id in self.module_aliases:
                return ("module", self.module_aliases[expr.id], self.module_alias_meta.get(expr.id, ""))
            if not ignore_locals and expr.id in self.local_names:
                return (None, None, "")
            symbol = self.analyzer.top_symbols_by_module.get(self.module_name, {}).get(expr.id)
            if symbol:
                return ("symbol", symbol, "")
            return (None, None, "")

        if isinstance(expr, ast.Attribute):
            if isinstance(expr.value, ast.Name) and expr.value.id in ("self", "cls") and self.class_symbol:
                if expr.attr in self.attribute_bindings:
                    return binding_parts(self.attribute_bindings[expr.attr])
                method = self.analyzer.methods_by_class_key.get(self.class_symbol["SymbolKey"], {}).get(expr.attr)
                if method:
                    return ("symbol", method, "")

            base_kind, base, base_meta = self.resolve_expr_meta(expr.value, ignore_locals=ignore_locals)
            if base_kind == "module" and base:
                submodule = normalize_module_name(
                    f"{base}.{expr.attr}",
                    self.analyzer.module_to_package,
                    self.analyzer.module_prefixes,
                )
                if submodule:
                    return ("module", submodule, base_meta)
                symbol = self.analyzer.top_symbols_by_module.get(base, {}).get(expr.attr)
                if symbol:
                    return ("symbol", symbol, base_meta)
                return (None, None, "")

            if base_kind in ("instance", "symbol") and base:
                class_symbol = base if base_kind == "instance" or base["Kind"] == "class" else None
                if class_symbol:
                    if expr.attr in self.analyzer.class_attribute_bindings.get(class_symbol["SymbolKey"], {}):
                        return binding_parts(self.analyzer.class_attribute_bindings[class_symbol["SymbolKey"]][expr.attr])
                    method = self.analyzer.methods_by_class_key.get(class_symbol["SymbolKey"], {}).get(expr.attr)
                    if method:
                        return ("symbol", method, base_meta)
            return (None, None, "")

        if isinstance(expr, ast.Call):
            module_name, meta = self.resolve_dynamic_module(expr)
            if module_name:
                return ("module", module_name, meta)
            kind, target, meta = self.resolve_expr_meta(expr.func, ignore_locals=ignore_locals)
            if kind == "symbol" and target and target["Kind"] == "class":
                return ("instance", target, meta)
            return (None, None, "")

        return (None, None, "")

    def resolve_dynamic_module(self, expr):
        if not isinstance(expr, ast.Call):
            return "", ""
        if not expr.args:
            return "", ""

        func = expr.func
        if isinstance(func, ast.Name):
            if func.id not in self.import_call_aliases:
                return "", ""
        elif isinstance(func, ast.Attribute):
            if func.attr != "import_module":
                return "", ""
            if not isinstance(func.value, ast.Name) or func.value.id not in self.module_loader_aliases:
                return "", ""
        else:
            return "", ""

        module_name = extract_string_value(expr.args[0])
        if not module_name:
            return "", ""

        if module_name.startswith("."):
            package_name = ""
            if len(expr.args) > 1:
                package_name = extract_string_value(expr.args[1])
            if not package_name:
                for keyword in expr.keywords:
                    if keyword.arg == "package":
                        package_name = extract_string_value(keyword.value)
                        break
            if not package_name:
                return "", ""

            level = len(module_name) - len(module_name.lstrip("."))
            module_name = resolve_import_from_module(package_name, module_name[level:], level)

        return (
            normalize_module_name(
                module_name,
                self.analyzer.module_to_package,
                self.analyzer.module_prefixes,
            ),
            "dynamic_import",
        )

    def receiver_label(self):
        if self.class_symbol:
            return self.class_symbol.get("Name", "") or "receiver"
        return "receiver"

    def flow_source(self, expr):
        if isinstance(expr, ast.Starred):
            return self.flow_source(expr.value)
        if isinstance(expr, ast.Attribute):
            return self.flow_source(expr.value)
        if isinstance(expr, ast.Subscript):
            return self.flow_source(expr.value)
        if isinstance(expr, ast.Name):
            if expr.id in ("self", "cls") and self.class_symbol:
                return "receiver", self.receiver_label()
            if expr.id in self.param_names:
                return "param", expr.id
        return "", ""


class RelationshipVisitor(ast.NodeVisitor):
    def __init__(self, state):
        self.state = state

    def visit_FunctionDef(self, node):
        return

    def visit_AsyncFunctionDef(self, node):
        return

    def visit_ClassDef(self, node):
        return

    def visit_If(self, node):
        if is_type_checking_guard(node.test):
            previous = self.state.in_type_checking
            self.state.in_type_checking = True
            for statement in node.body:
                self.visit(statement)
            self.state.in_type_checking = previous
            for statement in node.orelse:
                self.visit(statement)
            return
        self.generic_visit(node)

    def visit_annotation(self, node):
        previous = self.state.in_annotation
        self.state.in_annotation = True
        try:
            self.visit(node)
        finally:
            self.state.in_annotation = previous

    def visit_Import(self, node):
        for alias in node.names:
            if alias.name == "importlib":
                self.state.module_loader_aliases.add(alias.asname or "importlib")
                self.state.bind_name(alias.asname or alias.name.split(".")[0], None)
                continue
            bound_name = alias.asname or alias.name.split(".")[0]
            target_module = normalize_module_name(
                alias.name,
                self.state.analyzer.module_to_package,
                self.state.analyzer.module_prefixes,
            )
            if not target_module:
                continue
            module_ref = target_module if alias.asname else bound_name
            meta = "type_checking_import" if self.state.in_type_checking else "import"
            self.state.module_aliases[bound_name] = module_ref
            self.state.module_alias_meta[bound_name] = meta
            self.state.bind_name(bound_name, ("module", module_ref, meta))
            self.state.analyzer.add_module_dependency(self.state.package_path, target_module)

    def visit_ImportFrom(self, node):
        if node.level == 0 and (node.module or "") == "importlib":
            for alias in node.names:
                if alias.name == "import_module":
                    self.state.import_call_aliases.add(alias.asname or alias.name)
                    self.state.bind_name(alias.asname or alias.name, None)
            return

        base_module = resolve_import_from_module(
            self.state.package_path, node.module or "", node.level
        )
        base_module = normalize_module_name(
            base_module,
            self.state.analyzer.module_to_package,
            self.state.analyzer.module_prefixes,
        )
        if not base_module:
            return

        self.state.analyzer.add_module_dependency(self.state.package_path, base_module)
        for alias in node.names:
            if alias.name == "*":
                for export_name in self.state.analyzer._module_export_names(base_module):
                    symbol, export_meta = self.state.analyzer._resolve_module_export_detail(base_module, export_name)
                    if symbol:
                        meta = contextual_import_meta(export_meta, self.state.in_type_checking)
                        self.state.symbol_aliases[export_name] = symbol["SymbolKey"]
                        self.state.symbol_alias_meta[export_name] = meta
                        self.state.bind_name(export_name, ("symbol", symbol, meta))
                continue

            target_module = normalize_module_name(
                f"{base_module}.{alias.name}",
                self.state.analyzer.module_to_package,
                self.state.analyzer.module_prefixes,
            )
            if target_module:
                bound_name = alias.asname or alias.name
                meta = "type_checking_import" if self.state.in_type_checking else "import"
                self.state.module_aliases[bound_name] = target_module
                self.state.module_alias_meta[bound_name] = meta
                self.state.bind_name(bound_name, ("module", target_module, meta))
                self.state.analyzer.add_module_dependency(self.state.package_path, target_module)
                continue

            symbol, export_meta = self.state.analyzer._resolve_module_export_detail(base_module, alias.name)
            if symbol:
                bound_name = alias.asname or alias.name
                meta = contextual_import_meta(export_meta, self.state.in_type_checking)
                self.state.symbol_aliases[bound_name] = symbol["SymbolKey"]
                self.state.symbol_alias_meta[bound_name] = meta
                self.state.bind_name(bound_name, ("symbol", symbol, meta))

    def visit_Assign(self, node):
        self.visit(node.value)
        binding = self.state.binding_from_value(node.value)
        for target in node.targets:
            self.state.bind_target(target, binding)

    def visit_AnnAssign(self, node):
        annotation = normalize_annotation_expr(node.annotation)
        if annotation is not None:
            self.visit_annotation(annotation)
        if node.value is not None:
            self.visit(node.value)
        binding = self.state.binding_from_value(node.value, node.annotation)
        self.state.bind_target(node.target, binding)

    def visit_With(self, node):
        for item in node.items:
            self.visit(item.context_expr)
            binding = self.state.binding_from_value(item.context_expr)
            if item.optional_vars is not None:
                self.state.bind_target(item.optional_vars, binding)
        for statement in node.body:
            self.visit(statement)

    def visit_AsyncWith(self, node):
        self.visit_With(node)

    def visit_Name(self, node):
        if not isinstance(node.ctx, ast.Load):
            return
        kind, symbol, meta = self.state.resolve_expr_meta(node)
        if kind not in ("symbol", "instance"):
            return
        if symbol:
            self.state.analyzer.add_reference(
                self.state.package_path,
                self.state.owner,
                symbol,
                self.state.rel_path,
                node.lineno,
                node.col_offset + 1,
                reference_kind(symbol["Kind"], meta, self.state.in_annotation),
            )

    def visit_Attribute(self, node):
        kind, symbol, meta = self.state.resolve_expr_meta(node)
        if kind not in ("symbol", "instance"):
            self.generic_visit(node)
            return
        if symbol:
            self.state.analyzer.add_reference(
                self.state.package_path,
                self.state.owner,
                symbol,
                self.state.rel_path,
                node.lineno,
                node.col_offset + 1,
                reference_kind(symbol["Kind"], meta, self.state.in_annotation),
            )
        self.generic_visit(node)

    def visit_Call(self, node):
        kind, symbol, meta = self.state.resolve_expr_meta(node.func)
        if kind not in ("symbol", "instance"):
            self.generic_visit(node)
            return
        if symbol:
            self.state.analyzer.add_call(
                self.state.package_path,
                self.state.owner,
                symbol,
                self.state.rel_path,
                node.lineno,
                node.col_offset + 1,
                call_dispatch(symbol, meta),
            )
            flow_kind, flow_label = self.state.flow_source(node.func)
            if flow_kind and flow_label:
                self.state.analyzer.add_flow(
                    self.state.package_path,
                    self.state.owner,
                    self.state.rel_path,
                    node.lineno,
                    node.col_offset + 1,
                    f"{flow_kind}_to_call",
                    source_kind=flow_kind,
                    source_label=flow_label,
                    target_kind="call",
                    target_label=symbol.get("QName", ""),
                    target_symbol=symbol,
                )
            for arg in node.args:
                flow_kind, flow_label = self.state.flow_source(arg)
                if not flow_kind or not flow_label:
                    continue
                self.state.analyzer.add_flow(
                    self.state.package_path,
                    self.state.owner,
                    self.state.rel_path,
                    node.lineno,
                    node.col_offset + 1,
                    f"{flow_kind}_to_call",
                    source_kind=flow_kind,
                    source_label=flow_label,
                    target_kind="call",
                    target_label=symbol.get("QName", ""),
                    target_symbol=symbol,
                )
        self.generic_visit(node)

    def visit_Return(self, node):
        if node.value is not None:
            if isinstance(node.value, ast.Call):
                kind, symbol, _ = self.state.resolve_expr_meta(node.value.func)
                if kind in ("symbol", "instance") and symbol:
                    self.state.analyzer.add_flow(
                        self.state.package_path,
                        self.state.owner,
                        self.state.rel_path,
                        node.lineno,
                        node.col_offset + 1,
                        "call_to_return",
                        source_kind="call",
                        source_label=symbol.get("QName", ""),
                        source_symbol=symbol,
                        target_kind="return",
                        target_label="return",
                    )
                else:
                    flow_kind, flow_label = self.state.flow_source(node.value)
                    if flow_kind and flow_label:
                        self.state.analyzer.add_flow(
                            self.state.package_path,
                            self.state.owner,
                            self.state.rel_path,
                            node.lineno,
                            node.col_offset + 1,
                            f"{flow_kind}_to_return",
                            source_kind=flow_kind,
                            source_label=flow_label,
                            target_kind="return",
                            target_label="return",
                        )
            else:
                flow_kind, flow_label = self.state.flow_source(node.value)
                if flow_kind and flow_label:
                    self.state.analyzer.add_flow(
                        self.state.package_path,
                        self.state.owner,
                        self.state.rel_path,
                        node.lineno,
                        node.col_offset + 1,
                        f"{flow_kind}_to_return",
                        source_kind=flow_kind,
                        source_label=flow_label,
                        target_kind="return",
                        target_label="return",
                    )
        self.generic_visit(node)


def extract_string_value(node):
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return node.value
    return ""


def is_type_checking_guard(test):
    if isinstance(test, ast.Name):
        return test.id == "TYPE_CHECKING"
    if isinstance(test, ast.Attribute):
        return isinstance(test.value, ast.Name) and test.value.id == "typing" and test.attr == "TYPE_CHECKING"
    return False


def contextual_import_meta(export_meta, in_type_checking):
    if in_type_checking:
        if export_meta == "re_export":
            return "type_checking_reexport"
        return "type_checking_import"
    if export_meta:
        return export_meta
    return "import"


def call_dispatch(symbol, meta):
    if meta == "dynamic_import":
        return "dynamic_import"
    if meta in ("re_export", "type_checking_reexport"):
        return "reexport"
    if symbol and symbol.get("Kind") == "method":
        return "dynamic"
    return "static"
