from modules import normalize_name, trim_test_name


def link_tests(test_key, package_path, test_name, class_hint, package_symbols, all_symbols):
    base = trim_test_name(test_name)
    if not base:
        return []

    links = []
    normalized_base = normalize_name(base)
    base_parts = [part for part in base.split("_") if part]
    class_hint = normalize_name(class_hint)

    def add(symbol, link_kind, confidence):
        links.append(
            {
                "TestPackageImportPath": package_path,
                "TestKey": test_key,
                "SymbolKey": symbol["SymbolKey"],
                "LinkKind": link_kind,
                "Confidence": confidence,
            }
        )

    for symbol in package_symbols:
        if symbol["Kind"] == "method":
            receiver = normalize_name(symbol["Receiver"])
            if class_hint and receiver == class_hint and normalize_name(symbol["Name"]) == normalized_base:
                add(symbol, "receiver_match", "high")
            elif len(base_parts) >= 2 and receiver == normalize_name(base_parts[0]) and normalize_name(symbol["Name"]) == normalize_name("_".join(base_parts[1:])):
                add(symbol, "receiver_match", "high")

        if normalize_name(symbol["Name"]) == normalized_base:
            add(symbol, "name_match", "medium")

    if links:
        return links

    for symbol in all_symbols:
        if normalize_name(symbol["Name"]) == normalized_base:
            add(symbol, "global_name_match", "low")
    return links
