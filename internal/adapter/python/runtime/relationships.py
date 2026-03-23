import ast

from modules import normalize_module_name


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
    kind, value = binding
    if kind in ("symbol", "instance"):
        return value
    return None


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
        symbol_aliases,
        local_names,
    ):
        self.analyzer = analyzer
        self.rel_path = rel_path
        self.package_path = package_path
        self.module_name = module_name
        self.owner = owner
        self.class_symbol = class_symbol
        self.module_aliases = dict(module_aliases)
        self.symbol_aliases = dict(symbol_aliases)
        self.local_names = set(local_names)
        self.local_bindings = {}

        class_key = class_symbol["SymbolKey"] if class_symbol else ""
        self.attribute_bindings = dict(analyzer.class_attribute_bindings.get(class_key, {}))
        if class_symbol:
            self.bind_name("self", ("instance", class_symbol))
            self.bind_name("cls", ("symbol", class_symbol))

    def seed_function_args(self, args):
        for arg in list(args.posonlyargs) + list(args.args) + list(args.kwonlyargs):
            self.bind_name(arg.arg, self.binding_from_annotation(arg.annotation))
        if args.vararg:
            self.bind_name(args.vararg.arg, None)
        if args.kwarg:
            self.bind_name(args.kwarg.arg, None)

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
        if annotation is None:
            return None
        kind, value = self.resolve_expr(annotation, ignore_locals=True)
        if kind == "symbol" and value and value["Kind"] == "class":
            return ("instance", value)
        if kind == "instance" and value:
            return ("instance", value)
        return None

    def binding_from_value(self, value, annotation=None):
        if annotation is not None:
            binding = self.binding_from_annotation(annotation)
            if binding:
                return binding
        if value is None:
            return None
        kind, target = self.resolve_expr(value)
        if kind in ("module", "symbol", "instance"):
            return (kind, target)
        return None

    def resolve_expr(self, expr, ignore_locals=False):
        if isinstance(expr, ast.Name):
            if not ignore_locals and expr.id in self.local_bindings:
                return self.local_bindings[expr.id]
            if expr.id in self.symbol_aliases:
                return ("symbol", self.analyzer.symbols_by_key.get(self.symbol_aliases[expr.id]))
            if expr.id in self.module_aliases:
                return ("module", self.module_aliases[expr.id])
            if not ignore_locals and expr.id in self.local_names:
                return (None, None)
            symbol = self.analyzer.top_symbols_by_module.get(self.module_name, {}).get(expr.id)
            if symbol:
                return ("symbol", symbol)
            return (None, None)

        if isinstance(expr, ast.Attribute):
            if isinstance(expr.value, ast.Name) and expr.value.id in ("self", "cls") and self.class_symbol:
                if expr.attr in self.attribute_bindings:
                    return self.attribute_bindings[expr.attr]
                method = self.analyzer.methods_by_class_key.get(self.class_symbol["SymbolKey"], {}).get(expr.attr)
                if method:
                    return ("symbol", method)

            base_kind, base = self.resolve_expr(expr.value, ignore_locals=ignore_locals)
            if base_kind == "module" and base:
                submodule = normalize_module_name(f"{base}.{expr.attr}", self.analyzer.module_to_package)
                if submodule:
                    return ("module", submodule)
                symbol = self.analyzer.top_symbols_by_module.get(base, {}).get(expr.attr)
                if symbol:
                    return ("symbol", symbol)
                return (None, None)

            if base_kind in ("instance", "symbol") and base:
                class_symbol = base if base_kind == "instance" or base["Kind"] == "class" else None
                if class_symbol:
                    if expr.attr in self.analyzer.class_attribute_bindings.get(class_symbol["SymbolKey"], {}):
                        return self.analyzer.class_attribute_bindings[class_symbol["SymbolKey"]][expr.attr]
                    method = self.analyzer.methods_by_class_key.get(class_symbol["SymbolKey"], {}).get(expr.attr)
                    if method:
                        return ("symbol", method)
            return (None, None)

        if isinstance(expr, ast.Call):
            kind, target = self.resolve_expr(expr.func, ignore_locals=ignore_locals)
            if kind == "symbol" and target and target["Kind"] == "class":
                return ("instance", target)
            return (None, None)

        return (None, None)


class RelationshipVisitor(ast.NodeVisitor):
    def __init__(self, state):
        self.state = state

    def visit_FunctionDef(self, node):
        return

    def visit_AsyncFunctionDef(self, node):
        return

    def visit_ClassDef(self, node):
        return

    def visit_Import(self, node):
        for alias in node.names:
            bound_name = alias.asname or alias.name.split(".")[0]
            target_module = normalize_module_name(alias.name, self.state.analyzer.module_to_package)
            if not target_module:
                continue
            module_ref = target_module if alias.asname else bound_name
            self.state.bind_name(bound_name, ("module", module_ref))

    def visit_ImportFrom(self, node):
        from modules import resolve_import_from_module

        base_module = resolve_import_from_module(
            self.state.package_path, node.module or "", node.level
        )
        base_module = normalize_module_name(base_module, self.state.analyzer.module_to_package)
        if not base_module:
            return

        for alias in node.names:
            if alias.name == "*":
                continue

            target_module = normalize_module_name(
                f"{base_module}.{alias.name}", self.state.analyzer.module_to_package
            )
            if target_module:
                self.state.bind_name(alias.asname or alias.name, ("module", target_module))
                continue

            symbol = self.state.analyzer.top_symbols_by_module.get(base_module, {}).get(alias.name)
            if symbol:
                self.state.bind_name(alias.asname or alias.name, ("symbol", symbol))

    def visit_Assign(self, node):
        self.visit(node.value)
        binding = self.state.binding_from_value(node.value)
        for target in node.targets:
            self.state.bind_target(target, binding)

    def visit_AnnAssign(self, node):
        if node.annotation is not None:
            self.visit(node.annotation)
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
        symbol = binding_symbol(self.state.resolve_expr(node))
        if symbol:
            self.state.analyzer.add_reference(
                self.state.package_path,
                self.state.owner,
                symbol,
                self.state.rel_path,
                node.lineno,
                node.col_offset + 1,
            )

    def visit_Attribute(self, node):
        symbol = binding_symbol(self.state.resolve_expr(node))
        if symbol:
            self.state.analyzer.add_reference(
                self.state.package_path,
                self.state.owner,
                symbol,
                self.state.rel_path,
                node.lineno,
                node.col_offset + 1,
            )
        self.generic_visit(node)

    def visit_Call(self, node):
        symbol = binding_symbol(self.state.resolve_expr(node.func))
        if symbol:
            self.state.analyzer.add_call(
                self.state.package_path,
                self.state.owner,
                symbol,
                self.state.rel_path,
                node.lineno,
                node.col_offset + 1,
            )
        self.generic_visit(node)
