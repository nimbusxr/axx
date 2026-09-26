  if (language === "gherkin") {
    const m = /^\s*the\s+"((?:[^"\\]|\\.)*)"\s+(button|link|tab|menu item|checkbox|option|field|element)\s*$/.exec(locator2);
    if (m) {
      const name = JSON.parse('"' + m[1] + '"');
      if (name.startsWith("css=") || name.startsWith("xpath="))
        return name;
      if (name.startsWith("testid="))
        return getByTestIdSelector(testIdAttributeName2, name.slice("testid=".length));
      const roles = { button: "button", link: "link", tab: "tab", "menu item": "menuitem", checkbox: "checkbox", option: "radio" };
      if (roles[m[2]])
        return getByRoleSelector(roles[m[2]], { name, exact: true });
      if (m[2] === "field")
        return getByLabelSelector(name, { exact: true });
      return getByTextSelector(name, { exact: true });
    }
  }
