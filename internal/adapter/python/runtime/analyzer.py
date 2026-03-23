import ast

from modules import (
    is_python_test_class,
    is_python_test_name,
    normalize_module_name,
    python_module_import_path,
    python_package_import_path,
    resolve_import_from_module,
    trim_test_class_name,
)
from relationships import RelationshipVisitor, ScopeState, collect_local_names
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
        self.patterns = set(payload.get("patterns") or [])
        self.files = payload.get("files") or []

        self.source_files = []
        self.test_files = []
        self.file_meta = {}

        self.packages = {}
        self.module_to_package = {}
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

            try:
                with open(abs_path, "r", encoding="utf-8") as handle:
                    source = handle.read()
            except OSError as exc:
                raise SystemExit(f"read {rel_path}: {exc}") from exc

            try:
                tree = ast.parse(source, filename=abs_path, type_comments=True)
            except SyntaxError as exc:
                raise SystemExit(f"parse {rel_path}: {exc}") from exc

            info = {
                "rel_path": rel_path,
                "abs_path": abs_path,
                "is_test": is_test,
                "source": source,
                "tree": tree,
                "module_name": python_module_import_path(self.project_name, rel_path),
                "package_path": python_package_import_path(self.project_name, rel_path),
                "dir_path": rel_path.rsplit("/", 1)[0] if "/" in rel_path else ".",
            }
            self.file_meta[rel_path] = info
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

            module_aliases, symbol_aliases, dep_targets = self._resolve_module_imports(info)
            for target_module in dep_targets:
                target_package = self.module_to_package.get(target_module)
                if target_package:
                    self._add_dependency(info["package_path"], target_package)

            for node in info["tree"].body:
                if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                    owner = self.top_symbols_by_module[info["module_name"]].get(node.name)
                    if owner:
                        self._walk_owner_body(info, owner, node, None, module_aliases, symbol_aliases)
                elif isinstance(node, ast.ClassDef):
                    class_symbol = self.top_symbols_by_module[info["module_name"]].get(node.name)
                    if not class_symbol:
                        continue

                    self._walk_class_body(info, class_symbol, node, module_aliases, symbol_aliases)

                    method_nodes = [
                        child for child in node.body if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef))
                    ]
                    method_nodes.sort(key=lambda child: (child.name != "__init__", child.lineno))
                    for child in method_nodes:
                        owner = self.methods_by_class_key[class_symbol["SymbolKey"]].get(child.name)
                        if owner:
                            self._walk_owner_body(info, owner, child, class_symbol, module_aliases, symbol_aliases)

    def _resolve_module_imports(self, info):
        module_aliases = {}
        symbol_aliases = {}
        dep_targets = set()

        for node in info["tree"].body:
            if isinstance(node, ast.Import):
                for alias in node.names:
                    bound_name = alias.asname or alias.name.split(".")[0]
                    target_module = normalize_module_name(alias.name, self.module_to_package)
                    if not target_module:
                        target_module = normalize_module_name(bound_name, self.module_to_package)
                    if not target_module:
                        continue
                    module_aliases[bound_name] = target_module if alias.asname else bound_name
                    dep_targets.add(target_module)
            elif isinstance(node, ast.ImportFrom):
                base_module = resolve_import_from_module(
                    info["package_path"], node.module or "", node.level
                )
                base_module = normalize_module_name(base_module, self.module_to_package)
                if not base_module:
                    continue

                dep_targets.add(base_module)
                for alias in node.names:
                    if alias.name == "*":
                        continue

                    full_submodule = normalize_module_name(
                        f"{base_module}.{alias.name}", self.module_to_package
                    )
                    if full_submodule:
                        module_aliases[alias.asname or alias.name] = full_submodule
                        dep_targets.add(full_submodule)
                        continue

                    symbol = self.top_symbols_by_module.get(base_module, {}).get(alias.name)
                    if symbol:
                        symbol_aliases[alias.asname or alias.name] = symbol["SymbolKey"]

        return module_aliases, symbol_aliases, dep_targets

    def _walk_owner_body(self, info, owner, node, class_symbol, module_aliases, symbol_aliases):
        scope = ScopeState(
            analyzer=self,
            rel_path=info["rel_path"],
            package_path=info["package_path"],
            module_name=info["module_name"],
            owner=owner,
            class_symbol=class_symbol,
            module_aliases=module_aliases,
            symbol_aliases=symbol_aliases,
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
        if getattr(node, "returns", None) is not None:
            visitor.visit(node.returns)

    def _walk_class_body(self, info, owner, node, module_aliases, symbol_aliases):
        scope = ScopeState(
            analyzer=self,
            rel_path=info["rel_path"],
            package_path=info["package_path"],
            module_name=info["module_name"],
            owner=owner,
            class_symbol=owner,
            module_aliases=module_aliases,
            symbol_aliases=symbol_aliases,
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

    def add_reference(self, package_path, owner, target, rel_path, line, column):
        if not target:
            return
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
                "Kind": reference_kind(target["Kind"]),
            }
        )

    def add_call(self, package_path, owner, target, rel_path, line, column):
        if not target or target["Kind"] not in ("func", "method", "class"):
            return
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
                "Dispatch": "dynamic" if target["Kind"] == "method" else "static",
            }
        )

    def _should_emit_package(self, package_path):
        return not self.patterns or package_path in self.patterns


def symbol_module(symbol):
    parts = symbol["QName"].split(".")
    if symbol["Kind"] == "method":
        return ".".join(parts[:-2])
    return ".".join(parts[:-1])
