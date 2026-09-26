class {
  generateLocator(base, kind, body, options = {}) {
    const quoted = (text) => {
      if (text instanceof RegExp)
        throw new Error("no step finds an element by a pattern");
      return JSON.stringify(String(text));
    };
    switch (kind) {
      case "frame-locator":
      case "frame":
        return "\u0001";
      case "visible":
        return "";
      case "or":
        return "\u0003" + body;
      case "role": {
        if (options.name === undefined)
          throw new Error("no step finds an element by its role alone");
        const kinds = {
          button: "button", link: "link", tab: "tab", checkbox: "checkbox", radio: "option",
          menuitem: "menu item", menuitemcheckbox: "menu item", menuitemradio: "menu item",
          textbox: "field", searchbox: "field", combobox: "field", spinbutton: "field", listbox: "field"
        };
        return "the " + quoted(options.name) + " " + (kinds[body] || "element");
      }
      case "label":
      case "placeholder":
        return "the " + quoted(body) + " field";
      case "text":
      case "alt":
      case "title":
        return "the " + quoted(body) + " element";
      case "test-id":
        if (body instanceof RegExp)
          throw new Error("no step finds an element by a pattern");
        return "the " + quoted("testid=" + body) + " element";
      case "default": {
        if (options.hasText !== undefined || options.hasNotText !== undefined)
          throw new Error("no step finds an element by a selector and its text");
        const selector = String(body);
        if (selector === "visible=true")
          return "";
        if (selector.startsWith("xpath="))
          return "the " + quoted(selector) + " element";
        if (selector.startsWith("/") || selector.startsWith("("))
          return "the " + quoted("xpath=" + selector) + " element";
        if (selector.startsWith("css="))
          return "the " + quoted(selector) + " element";
        if (selector.startsWith("internal:") || /^[a-z_-]+=/.test(selector))
          throw new Error("no step finds an element with this selector engine");
        return "the " + quoted("css=" + selector) + " element";
      }
      default:
        throw new Error("no step finds an element this way: " + kind);
    }
  }
  chainLocators(locators) {
    // Steps look in every frame: what finds the frame goes.
    const frame = locators.lastIndexOf("\u0001");
    const name = (locator) => (/^the ("(?:[^"\\]|\\.)*") /.exec(locator) || [])[1];
    const parts = [];
    for (const locator of locators.slice(frame + 1)) {
      if (!locator)
        continue;
      // Another way to find what has the same name, as steps do (a field's
      // label or placeholder): the same element.
      if (locator.startsWith("\u0003")) {
        const previous = parts[parts.length - 1];
        if (previous && name(previous) !== undefined && name(previous) === name(locator.slice(1)))
          continue;
        throw new Error("no step finds an element by one name or another");
      }
      parts.push(locator);
    }
    if (!parts.length && frame >= 0)
      return "\u0001";
    if (parts.length !== 1)
      throw new Error("no step finds an element inside another");
    return parts[0];
  }
}
