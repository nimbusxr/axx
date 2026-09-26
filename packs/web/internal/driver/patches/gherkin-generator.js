class {
  constructor() {
    this.id = "axx";
    this.groupName = "axx";
    this.name = "Gherkin (axx steps)";
    this.highlighter = "gherkin";
  }
  reset() {
    this._previous = "";
    this._base = "";
  }
  generateHeader(options) {
    const base = options && options.contextOptions && options.contextOptions.baseURL;
    this._base = base ? String(base).replace(/\/+$/, "") : "";
    return "    # The steps, as you use the page: copy them into a scenario.";
  }
  generateFooter() {
    return "";
  }
  generateAction(actionInContext) {
    const action = actionInContext.action;
    const lines = [];
    const signals = action.signals || [];
    for (const [kind, text] of this._steps(action))
      lines.push([kind, text]);
    if (lines.length && signals.some((s) => s.name === "dialog"))
      lines.push(["when", "the dialog is dismissed"]);
    return lines.map(([kind, text]) => {
      if (kind === "comment")
        return "    # " + text;
      const keyword = kind === this._previous ? "And" : kind === "then" ? "Then" : "When";
      this._previous = kind;
      return "    " + keyword + " " + text;
    }).join("\n");
  }
  _steps(action) {
    const q = (text) => JSON.stringify(String(text));
    const page = (url) => {
      let path = String(url);
      if (this._base && (path === this._base || path.startsWith(this._base + "/") || path.startsWith(this._base + "?")))
        path = "/" + path.slice(this._base.length).replace(/^\/+/, "");
      return [["when", "the " + q(path) + " page is opened"]];
    };
    switch (action.name) {
      case "openPage":
        return action.url && action.url !== "about:blank" && action.url !== "chrome://newtab/" ? page(action.url) : [];
      case "navigate":
        return page(action.url);
      case "closePage":
        return [["when", "the browser tab is closed"]];
    }
    const target = asLocator("gherkin", action.selector);
    const m = /^the ("(?:[^"\\]|\\.)*") (button|link|tab|menu item|checkbox|option|field|element)$/.exec(target);
    if (!m) {
      return [["comment", action.name + ": no step finds this element by what people see; give it a label, a name or a test id (Playwright: " + asLocator("javascript", action.selector) + ")"]];
    }
    const [, name, kind] = m;
    switch (action.name) {
      case "click":
        if (action.button === "right")
          return [["when", target + " is right-clicked"]];
        if (action.clickCount === 2)
          return [["when", target + " is double-clicked"]];
        if (kind === "field")
          return [];
        if (kind === "option")
          return [["when", target + " is chosen"]];
        if (kind === "checkbox")
          return [["when", target + " is checked"]];
        return [["when", target + " is clicked"]];
      case "hover":
        return [["when", "the pointer is moved over " + name]];
      case "check":
        return [["when", kind === "option" ? target + " is chosen" : "the " + name + " checkbox is checked"]];
      case "uncheck":
        return [["when", "the " + name + " checkbox is unchecked"]];
      case "fill":
        return [["when", "the " + name + " field is filled with " + q(action.text)]];
      case "press": {
        const modifiers = [[1, "Alt"], [2, "Control"], [4, "Meta"], [8, "Shift"]].filter(([bit]) => (action.modifiers || 0) & bit).map(([, key]) => key);
        const key = [...modifiers, action.key].join("+");
        return [["when", "the " + key + " key is pressed in the " + name + " field"]];
      }
      case "select": {
        const options = action.options || [];
        if (options.length === 1)
          return [["when", q(options[0]) + " is chosen in the " + name + " field"]];
        return [["when", "the following options are chosen in the " + name + " field:\n" + options.map((o) => "      | " + String(o).replace(/\|/g, "\\|") + " |").join("\n")]];
      }
      case "setInputFiles":
        // The step takes a path of the project, without quotes; the page knows only the file's name.
        return (action.files || []).flatMap((file) => [
          ["when", "the " + file + " file is uploaded in the " + name + " field"],
          ["comment", file + ": the browser gives only the file's name; give its path in the project (relative to axx.yaml's directory or a resources directory, with no spaces)"],
        ]);
      case "assertText":
        return [["then", target + " shows " + q(action.text)]];
      case "assertVisible":
        return [["then", target + " is shown"]];
      case "assertValue":
        return [["then", "the " + name + " field has the value " + q(action.value)]];
      case "assertChecked":
        if (kind === "option")
          return [["then", target + (action.checked ? " is selected" : " is not selected")]];
        return [["then", "the " + name + " checkbox is " + (action.checked ? "ticked" : "not ticked")]];
      case "assertSnapshot": {
        // The web-a11y pack's step, with the snapshot as its doc string.
        const snapshot = String(action.ariaSnapshot || "").trim().split("\n").map((l) => "      " + l).join("\n");
        return [["then", target + "'s accessible structure is:\n      \"\"\"\n" + snapshot + "\n      \"\"\""]];
      }
    }
    return [["comment", action.name + " has no step"]];
  }
}
