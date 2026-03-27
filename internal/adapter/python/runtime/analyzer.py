import ast
import tokenize

from modules import (
    is_python_test_class,
    is_python_test_name,
    normalize_module_name,
    python_module_import_path,
    python_package_import_path,
    resolve_import_from_module,
    trim_test_class_name,
)
from relationships import (
    RelationshipVisitor,
    ScopeState,
    collect_local_names,
    normalize_annotation_expr,
)
from symbols import (
    build_class_symbol,
    build_function_symbol,
    build_method_symbol,
    package_basename,
    reference_kind,
)
from testlinks import link_tests


class PythonAnalyzer:
    def __init__(self, payload):
        self.root = payload["root"]
        self.project_name = payload.get("project_name", "")
        self.source_roots = payload.get("source_roots") or ["src"]
        self.patterns = set(payload.get("patterns") or [])
        self.files = payload.get("files") or []

        self.source_files = []
        self.test_files = []
        self.file_meta = {}
        self.module_info = {}
        self.skipped_files = []

        self.packages = {}
        self.module_to_package = {}
        self.module_prefixes = set()
        self.module_export_cache = {}
        self.module_all_cache = {}
        self.top_symbols_by_module = {}
        self.classes_by_key = {}
        self.methods_by_class_key = {}
        self.class_attribute_bindings = {}
        self.symbols_by_key = {}
        self.all_symbols = []

        self.dependencies = []
        self.references = []
        self.calls = []
        self.tests = []
        self.test_links = []

        self._dep_seen = set()
        self._ref_seen = set()
        self._call_seen = set()
        self._test_link_seen = set()

    def run(self):
        self._parse_files()
        self._index_symbols()
        self._collect_relationships()
        self._collect_tests()

        packages = [
            item
            for key, item in self.packages.items()
            if self._should_emit_package(key)
        ]
        symbols = [
            symbol
            for symbol in self.all_symbols
            if self._should_emit_package(symbol["PackageImportPath"])
        ]

        impacted = sorted(self.patterns) if self.patterns else sorted(self.packages.keys())

        return {
            "packages": packages,
            "files": [],
            "symbols": symbols,
            "dependencies": self.dependencies,
            "references": self.references,
            "calls": self.calls,
            "tests": self.tests,
            "test_links": self.test_links,
            "impacted_packages": impacted,
        }

    def _parse_files(self):
        for item in self.files:
            rel_path = item["rel_path"]
            abs_path = item["abs_path"]
            is_test = bool(item.get("is_test"))
            info = {
                "rel_path": rel_path,
                "abs_path": abs_path,
                "is_test": is_test,
                "source": "",
                "tree": None,
                "parse_error": "",
                "module_name": python_module_import_path(self.project_name, rel_path, self.source_roots),
                "package_path": python_package_import_path(self.project_name, rel_path, self.source_roots),
                "dir_path": rel_path.rsplit("/", 1)[0] if "/" in rel_path else ".",
            }

            try:
                with tokenize.open(abs_path) as handle:
                    source = handle.read()
            except (OSError, SyntaxError, UnicodeDecodeError, LookupError) as exc:
                info["parse_error"] = f"read {rel_path}: {exc}"
                self.file_meta[rel_path] = info
                self.skipped_files.append(info)
                continue

            try:
                tree = ast.parse(source, filename=abs_path, type_comments=True)
            except (SyntaxError, ValueError) as exc:
                info["source"] = source
                info["parse_error"] = f"parse {rel_path}: {exc}"
                self.file_meta[rel_path] = info
                self.skipped_files.append(info)
                continue

            info["source"] = source
            info["tree"] = tree
            self.file_meta[rel_path] = info
            self.module_info[info["module_name"]] = info
            if is_test:
                self.test_files.append(info)
            else:
                self.source_files.append(info)

    def _index_symbols(self):
        for info in self.source_files:
            package_path = info["package_path"]
            module_name = info["module_name"]

            package = self.packages.setdefault(
                package_path,
                {
                    "ImportPath": package_path,
                    "Name": package_basename(package_path, module_name),
                    "DirPath": info["dir_path"],
                    "FileCount": 0,
                },
            )
            package["FileCount"] += 1
            self.module_to_package[module_name] = package_path
            self._register_module_prefixes(module_name)
            self.top_symbols_by_module.setdefault(module_name, {})

            for node in info["tree"].body:
                if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                    self._register_symbol(build_function_symbol(info, module_name, node))
                elif isinstance(node, ast.ClassDef):
                    class_symbol = build_class_symbol(info, module_name, node)
                    self._register_symbol(class_symbol)
                    self.classes_by_key[class_symbol["SymbolKey"]] = class_symbol
                    self.methods_by_class_key.setdefault(class_symbol["SymbolKey"], {})

                    for child in node.body:
                        if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)):
                            method_symbol = build_method_symbol(info, module_name, node, child)
                            self._register_symbol(method_symbol)
                            self.methods_by_class_key[class_symbol["SymbolKey"]][method_symbol["Name"]] = method_symbol

    def _register_symbol(self, symbol):
        self.symbols_by_key[symbol["SymbolKey"]] = symbol
        self.all_symbols.append(symbol)
        if symbol["Kind"] in ("func", "class"):
            self.top_symbols_by_module.setdefault(symbol_module(symbol), {})[symbol["Name"]] = symbol

    def _collect_relationships(self):
        for info in self.source_files:
            if not self._should_emit_package(info["package_path"]):
                continue

            (
                module_aliases,
                module_alias_meta,
                symbol_aliases,
                symbol_alias_meta,
                dep_targets,
                module_loader_aliases,
                import_call_aliases,
            ) = self._resolve_module_imports(info)
            for target_module in dep_targets:
                target_package = self._package_for_module(target_module)
                if target_package:
                    self._add_dependency(info["package_path"], target_package)

            for node in info["tree"].body:
                if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                    owner = self.top_symbols_by_module[info["module_name"]].get(node.name)
                    if owner:
                        self._walk_owner_body(
                            info,
                            owner,
                            node,
                            None,
                            module_aliases,
                            module_alias_meta,
                            symbol_aliases,
                            symbol_alias_meta,
                            module_loader_aliases,
                            import_call_aliases,
                        )
                elif isinstance(node, ast.ClassDef):
                    class_symbol = self.top_symbols_by_module[info["module_name"]].get(node.name)
                    if not class_symbol:
                        continue

                    self._walk_class_body(
                        info,
                        class_symbol,
                        node,
                        module_aliases,
                        module_alias_meta,
                        symbol_aliases,
                        symbol_alias_meta,
                        module_loader_aliases,
                        import_call_aliases,
                    )

                    method_nodes = [
                        child for child in node.body if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef))
                    ]
                    method_nodes.sort(key=lambda child: (child.name != "__init__", child.lineno))
                    for child in method_nodes:
                        owner = self.methods_by_class_key[class_symbol["SymbolKey"]].get(child.name)
                        if owner:
                            self._walk_owner_body(
                                info,
                                owner,
                                child,
                                class_symbol,
                                module_aliases,
                                module_alias_meta,
                                symbol_aliases,
                                symbol_alias_meta,
                                module_loader_aliases,
                                import_call_aliases,
                            )

    def _resolve_module_imports(self, info):
        module_aliases = {}
        module_alias_meta = {}
        symbol_aliases = {}
        symbol_alias_meta = {}
        dep_targets = set()
        module_loader_aliases = set()
        import_call_aliases = {"__import__"}

        for node, in_type_checking in iter_module_scope_import_entries(info["tree"].body):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    if alias.name == "importlib":
                        module_loader_aliases.add(alias.asname or "importlib")
                        continue
                    bound_name = alias.asname or alias.name.split(".")[0]
                    target_module = normalize_module_name(
                        alias.name, self.module_to_package, self.module_prefixes
                    )
                    if not target_module:
                        target_module = normalize_module_name(
                            bound_name, self.module_to_package, self.module_prefixes
                        )
                    if not target_module:
                        continue
                    module_aliases[bound_name] = target_module if alias.asname else bound_name
                    module_alias_meta[bound_name] = "type_checking_import" if in_type_checking else "import"
                    dep_targets.add(target_module)
            elif isinstance(node, ast.ImportFrom):
                if node.level == 0 and (node.module or "") == "importlib":
                    for alias in node.names:
                        if alias.name == "import_module":
                            import_call_aliases.add(alias.asname or alias.name)
                    continue

                base_module = resolve_import_from_module(
                    info["package_path"], node.module or "", node.level
                )
                base_module = normalize_module_name(
                    base_module, self.module_to_package, self.module_prefixes
                )
                if not base_module:
                    continue

                dep_targets.add(base_module)
                for alias in node.names:
                    if alias.name == "*":
                        for export_name in self._module_export_names(base_module):
                            symbol, export_meta = self._resolve_module_export_detail(base_module, export_name)
                            if symbol:
                                symbol_aliases[export_name] = symbol["SymbolKey"]
                                symbol_alias_meta[export_name] = contextual_import_meta(export_meta, in_type_checking)
                        continue

                    full_submodule = normalize_module_name(
                        f"{base_module}.{alias.name}",
                        self.module_to_package,
                        self.module_prefixes,
                    )
                    if full_submodule:
                        bound_name = alias.asname or alias.name
                        module_aliases[bound_name] = full_submodule
                        module_alias_meta[bound_name] = "type_checking_import" if in_type_checking else "import"
                        dep_targets.add(full_submodule)
                        continue

                    symbol, export_meta = self._resolve_module_export_detail(base_module, alias.name)
                    if symbol:
                        bound_name = alias.asname or alias.name
                        symbol_aliases[bound_name] = symbol["SymbolKey"]
                        symbol_alias_meta[bound_name] = contextual_import_meta(export_meta, in_type_checking)

        return (
            module_aliases,
            module_alias_meta,
            symbol_aliases,
            symbol_alias_meta,
            dep_targets,
            module_loader_aliases,
            import_call_aliases,
        )

    def _register_module_prefixes(self, module_name):
        parts = [part for part in (module_name or "").split(".") if part]
        for idx in range(1, len(parts) + 1):
            self.module_prefixes.add(".".join(parts[:idx]))

    def _package_for_module(self, module_name):
        module_name = module_name or ""
        if module_name in self.module_to_package:
            return self.module_to_package[module_name]
        best_match = ""
        best_package = ""
        for candidate, package_path in self.module_to_package.items():
            if not candidate.startswith(module_name + "."):
                continue
            if len(candidate) > len(best_match):
                best_match = candidate
                best_package = package_path
        return best_package

    def _resolve_module_export(self, module_name, name, seen=None):
        symbol, _ = self._resolve_module_export_detail(module_name, name, seen)
        return symbol

    def _resolve_module_export_detail(self, module_name, name, seen=None):
        if not module_name or not name:
            return None, ""

        cache = self.module_export_cache.setdefault(module_name, {})
        if name in cache:
            return cache[name]

        if seen is None:
            seen = set()
        key = (module_name, name)
        if key in seen:
            return None, ""
        seen.add(key)

        symbol = self.top_symbols_by_module.get(module_name, {}).get(name)
        if symbol:
            cache[name] = (symbol, "")
            return cache[name]

        info = self.module_info.get(module_name)
        if not info or not info.get("tree"):
            return None, ""

        for node in iter_module_scope_imports(info["tree"].body):
            if not isinstance(node, ast.ImportFrom):
                continue

            base_module = resolve_import_from_module(
                info["package_path"], node.module or "", node.level
            )
            base_module = normalize_module_name(
                base_module, self.module_to_package, self.module_prefixes
            )
            if not base_module:
                continue

            for alias in node.names:
                bound_name = alias.asname or alias.name
                if alias.name == "*":
                    if name.startswith("_") and name not in self._module_all_names(base_module):
                        continue
                    symbol, _ = self._resolve_module_export_detail(base_module, name, seen)
                    if symbol:
                        cache[name] = (symbol, "re_export")
                        return cache[name]
                    continue
                if bound_name != name:
                    continue

                full_submodule = normalize_module_name(
                    f"{base_module}.{alias.name}",
                    self.module_to_package,
                    self.module_prefixes,
                )
                if full_submodule:
                    cache[name] = (None, "")
                    return cache[name]

                symbol, _ = self._resolve_module_export_detail(base_module, alias.name, seen)
                if symbol:
                    cache[name] = (symbol, "re_export")
                    return cache[name]

        symbol, meta = self._resolve_assigned_module_export(info, name, seen)
        if symbol:
            cache[name] = (symbol, meta)
            return cache[name]

        cache[name] = (None, "")
        return cache[name]

    def _resolve_assigned_module_export(self, info, name, seen):
        module_aliases, module_alias_meta, symbol_aliases, symbol_alias_meta, _, _, _ = self._resolve_module_imports(info)
        for node in info["tree"].body:
            targets = []
            value = None
            if isinstance(node, ast.Assign):
                targets = node.targets
                value = node.value
            elif isinstance(node, ast.AnnAssign):
                targets = [node.target]
                value = node.value
            if value is None:
                continue

            for target in targets:
                if isinstance(target, ast.Name) and target.id == name:
                    kind, resolved, meta = self._resolve_module_export_binding(
                        info["module_name"],
                        value,
                        module_aliases,
                        module_alias_meta,
                        symbol_aliases,
                        symbol_alias_meta,
                        seen,
                    )
                    if kind == "symbol":
                        return resolved, "re_export" if meta in ("import", "re_export", "type_checking_import", "type_checking_reexport") else meta
        return None, ""

    def _resolve_module_export_binding(
        self,
        module_name,
        expr,
        module_aliases,
        module_alias_meta,
        symbol_aliases,
        symbol_alias_meta,
        seen,
    ):
        expr = normalize_annotation_expr(expr)
        if expr is None:
            return None, None, ""

        if isinstance(expr, ast.Name):
            if expr.id in symbol_aliases:
                symbol = self.symbols_by_key.get(symbol_aliases[expr.id])
                if symbol:
                    return "symbol", symbol, symbol_alias_meta.get(expr.id, "")
            if expr.id in module_aliases:
                return "module", module_aliases[expr.id], module_alias_meta.get(expr.id, "")
            symbol = self.top_symbols_by_module.get(module_name, {}).get(expr.id)
            if symbol:
                return "symbol", symbol, ""
            if expr.id != module_name.rsplit(".", 1)[-1]:
                symbol, meta = self._resolve_module_export_detail(module_name, expr.id, seen)
                if symbol:
                    return "symbol", symbol, meta
            return None, None, ""

        if isinstance(expr, ast.Attribute):
            kind, resolved, meta = self._resolve_module_export_binding(
                module_name,
                expr.value,
                module_aliases,
                module_alias_meta,
                symbol_aliases,
                symbol_alias_meta,
                seen,
            )
            if kind == "module" and resolved:
                submodule = normalize_module_name(
                    f"{resolved}.{expr.attr}",
                    self.module_to_package,
                    self.module_prefixes,
                )
                if submodule:
                    return "module", submodule, meta
                symbol, symbol_meta = self._resolve_module_export_detail(resolved, expr.attr, seen)
                if symbol:
                    return "symbol", symbol, symbol_meta or meta
            return None, None, ""

        return None, None, ""

    def _module_all_names(self, module_name):
        if module_name in self.module_all_cache:
            return self.module_all_cache[module_name]

        info = self.module_info.get(module_name)
        if not info or not info.get("tree"):
            self.module_all_cache[module_name] = set()
            return self.module_all_cache[module_name]

        names = set()
        for node in info["tree"].body:
            if isinstance(node, ast.Assign):
                for target in node.targets:
                    if isinstance(target, ast.Name) and target.id == "__all__":
                        names.update(extract_string_names(node.value))
            elif isinstance(node, ast.AugAssign):
                if isinstance(node.target, ast.Name) and node.target.id == "__all__":
                    names.update(extract_string_names(node.value))
            elif isinstance(node, ast.Expr) and isinstance(node.value, ast.Call):
                call = node.value
                if (
                    isinstance(call.func, ast.Attribute)
                    and isinstance(call.func.value, ast.Name)
                    and call.func.value.id == "__all__"
                    and call.func.attr == "extend"
                    and len(call.args) == 1
                ):
                    names.update(extract_string_names(call.args[0]))

        self.module_all_cache[module_name] = names
        return names

    def _module_export_names(self, module_name):
        names = set(self.top_symbols_by_module.get(module_name, {}).keys())
        info = self.module_info.get(module_name)
        if not info or not info.get("tree"):
            return sorted(names)

        all_names = self._module_all_names(module_name)
        if all_names:
            names.update(all_names)
            return sorted(names)

        for node in iter_module_scope_imports(info["tree"].body):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    bound_name = alias.asname or alias.name.split(".")[0]
                    if bound_name and not bound_name.startswith("_"):
                        names.add(bound_name)
            elif isinstance(node, ast.ImportFrom):
                for alias in node.names:
                    bound_name = alias.asname or alias.name
                    if alias.name == "*":
                        continue
                    if bound_name and not bound_name.startswith("_"):
                        names.add(bound_name)
        return sorted(names)

    def _walk_owner_body(
        self,
        info,
        owner,
        node,
        class_symbol,
        module_aliases,
        module_alias_meta,
        symbol_aliases,
        symbol_alias_meta,
        module_loader_aliases,
        import_call_aliases,
    ):
        scope = ScopeState(
            analyzer=self,
            rel_path=info["rel_path"],
            package_path=info["package_path"],
            module_name=info["module_name"],
            owner=owner,
            class_symbol=class_symbol,
            module_aliases=module_aliases,
            module_alias_meta=module_alias_meta,
            symbol_aliases=symbol_aliases,
            symbol_alias_meta=symbol_alias_meta,
            module_loader_aliases=module_loader_aliases,
            import_call_aliases=import_call_aliases,
            local_names=collect_local_names(node),
        )
        scope.seed_function_args(node.args)

        visitor = RelationshipVisitor(scope)
        for statement in node.body:
            if isinstance(statement, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                continue
            visitor.visit(statement)

        for decorator in getattr(node, "decorator_list", []):
            visitor.visit(decorator)
        for annotation in iter_function_annotations(node.args):
            visitor.visit_annotation(annotation)
        for default in list(node.args.defaults) + [item for item in node.args.kw_defaults if item is not None]:
            visitor.visit(default)
        if getattr(node, "returns", None) is not None:
            visitor.visit_annotation(node.returns)

    def _walk_class_body(
        self,
        info,
        owner,
        node,
        module_aliases,
        module_alias_meta,
        symbol_aliases,
        symbol_alias_meta,
        module_loader_aliases,
        import_call_aliases,
    ):
        scope = ScopeState(
            analyzer=self,
            rel_path=info["rel_path"],
            package_path=info["package_path"],
            module_name=info["module_name"],
            owner=owner,
            class_symbol=owner,
            module_aliases=module_aliases,
            module_alias_meta=module_alias_meta,
            symbol_aliases=symbol_aliases,
            symbol_alias_meta=symbol_alias_meta,
            module_loader_aliases=module_loader_aliases,
            import_call_aliases=import_call_aliases,
            local_names=set(),
        )
        visitor = RelationshipVisitor(scope)

        for base in node.bases:
            visitor.visit(base)
        for decorator in node.decorator_list:
            visitor.visit(decorator)
        for statement in node.body:
            if isinstance(statement, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                continue
            visitor.visit(statement)

    def _collect_tests(self):
        package_symbols = {}
        for symbol in self.all_symbols:
            package_symbols.setdefault(symbol["PackageImportPath"], []).append(symbol)

        for info in self.test_files:
            if not self._should_emit_package(info["package_path"]):
                continue

            for node in info["tree"].body:
                if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and is_python_test_name(node.name):
                    self._add_test(info, node, None, package_symbols)
                elif isinstance(node, ast.ClassDef) and is_python_test_class(node):
                    class_hint = trim_test_class_name(node.name)
                    for child in node.body:
                        if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)) and is_python_test_name(child.name):
                            self._add_test(info, child, class_hint, package_symbols)

    def _add_test(self, info, node, class_hint, package_symbols):
        display_name = node.name if not class_hint else f"{class_hint}.{node.name}"
        test_key = f"test|{info['package_path']}|{info['rel_path']}|{display_name}"
        self.tests.append(
            {
                "TestKey": test_key,
                "PackageImportPath": info["package_path"],
                "FilePath": info["rel_path"],
                "Name": display_name,
                "Kind": "test",
                "Line": node.lineno,
            }
        )

        links = link_tests(
            test_key=test_key,
            package_path=info["package_path"],
            test_name=node.name,
            class_hint=class_hint,
            package_symbols=package_symbols.get(info["package_path"], []),
            all_symbols=self.all_symbols,
        )
        for link in links:
            key = (link["TestKey"], link["SymbolKey"], link["LinkKind"])
            if key in self._test_link_seen:
                continue
            self._test_link_seen.add(key)
            self.test_links.append(link)

    def _add_dependency(self, from_package, to_package):
        if not from_package or not to_package:
            return
        if from_package == to_package:
            return
        key = (from_package, to_package)
        if key in self._dep_seen:
            return
        self._dep_seen.add(key)
        self.dependencies.append(
            {
                "FromPackageImportPath": from_package,
                "ToPackageImportPath": to_package,
                "IsLocal": True,
            }
        )

    def add_reference(self, package_path, owner, target, rel_path, line, column, kind=None):
        if not target:
            return
        self._add_dependency(package_path, target.get("PackageImportPath"))
        key = (owner["SymbolKey"], target["SymbolKey"], rel_path, line, column)
        if key in self._ref_seen:
            return
        self._ref_seen.add(key)
        self.references.append(
            {
                "FromPackageImportPath": package_path,
                "FromSymbolKey": owner["SymbolKey"],
                "ToSymbolKey": target["SymbolKey"],
                "FilePath": rel_path,
                "Line": line,
                "Column": column,
                "Kind": kind or reference_kind(target["Kind"]),
            }
        )

    def add_call(self, package_path, owner, target, rel_path, line, column, dispatch=""):
        if not target or target["Kind"] not in ("func", "method", "class"):
            return
        self._add_dependency(package_path, target.get("PackageImportPath"))
        key = (owner["SymbolKey"], target["SymbolKey"], rel_path, line, column)
        if key in self._call_seen:
            return
        self._call_seen.add(key)
        self.calls.append(
            {
                "CallerPackageImportPath": package_path,
                "CallerSymbolKey": owner["SymbolKey"],
                "CalleeSymbolKey": target["SymbolKey"],
                "FilePath": rel_path,
                "Line": line,
                "Column": column,
                "Dispatch": dispatch or ("dynamic" if target["Kind"] == "method" else "static"),
            }
        )

    def add_module_dependency(self, from_package, target_module):
        target_package = self._package_for_module(target_module)
        if target_package:
            self._add_dependency(from_package, target_package)

    def _should_emit_package(self, package_path):
        return not self.patterns or package_path in self.patterns


def symbol_module(symbol):
    parts = symbol["QName"].split(".")
    if symbol["Kind"] == "method":
        return ".".join(parts[:-2])
    return ".".join(parts[:-1])


def iter_module_scope_imports(statements):
    for node, _ in iter_module_scope_import_entries(statements):
        yield node


def iter_module_scope_import_entries(statements, in_type_checking=False):
    for node in statements:
        if isinstance(node, (ast.Import, ast.ImportFrom)):
            yield node, in_type_checking
            continue
        if isinstance(node, ast.If) and is_type_checking_guard(node.test):
            for inner in iter_module_scope_import_entries(node.body, True):
                yield inner


def is_type_checking_guard(test):
    if isinstance(test, ast.Name):
        return test.id == "TYPE_CHECKING"
    if isinstance(test, ast.Attribute):
        return isinstance(test.value, ast.Name) and test.value.id == "typing" and test.attr == "TYPE_CHECKING"
    return False


def extract_string_names(node):
    if isinstance(node, (ast.List, ast.Tuple, ast.Set)):
        result = []
        for item in node.elts:
            if isinstance(item, ast.Constant) and isinstance(item.value, str):
                result.append(item.value)
        return result
    return []


def iter_function_annotations(args):
    for arg in list(args.posonlyargs) + list(args.args) + list(args.kwonlyargs):
        annotation = normalize_annotation_expr(arg.annotation)
        if annotation is not None:
            yield annotation
    if args.vararg:
        annotation = normalize_annotation_expr(args.vararg.annotation)
        if annotation is not None:
            yield annotation
    if args.kwarg:
        annotation = normalize_annotation_expr(args.kwarg.annotation)
        if annotation is not None:
            yield annotation


def contextual_import_meta(export_meta, in_type_checking):
    if in_type_checking:
        if export_meta == "re_export":
            return "type_checking_reexport"
        return "type_checking_import"
    if export_meta:
        return export_meta
    return "import"
