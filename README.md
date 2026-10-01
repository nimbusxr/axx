```text
     +@#%%@%     %%@#%.   %%%@% -#%%@#   .@%%@*
     @%%@#%%-     @%%@#  *%%@%.  +@#%%+  @%%@#
    +%%@%%@#%      %@%%@:%%@%-    *%@#%.@%%@%
    %%@%=@%%@.      %%@%%@#%=      %%%@#%%@%
   -%@%% %%@%%       @%%@%%*        %@%%@#%
   @#%%@  @%%@.       %@%%@          %%@%%
  -%%@#   %%@%%      %@%%@%%        %%@%%@%
  %%@%%@#%%@%%@     @#%%@%%@%      %%@%%@%%@
 :%@%%@%%@#%%@%%   @%%@# %@%%@    %@#%% %%@%%
 %@%%@     %#%%@  @%%@#   %%@%%  %@%%@   @%%@%
.#%%@+      %@#%%@%%@%    .#%%@%%@%%@     %@%%@
```

# axx

**axx** ("axxeptance") is a human-readable acceptance testing framework for the agentic era.

**Using axx?** Everything is at **[axx.nimbusxr.us](https://axx.nimbusxr.us)**: installing it, the
guides, and every step.

## Work on axx

You need [mise](https://mise.jdx.dev), and Docker for the integration tests.

```sh
git clone https://github.com/nimbusxr/axx && cd axx
mise install                    # Go and the tools, at the versions the repository pins
go build -o bin/axx ./cmd/axx   # axx
mise run test                   # go test -race ./...
mise run lint
mise run generate               # after changing steps, config or schemas; generated files are committed
```

- [AGENTS.md](AGENTS.md): where things are, and the rules every change follows.
- [CONTRIBUTING.md](CONTRIBUTING.md): sign off each commit (`git commit -s`), and title pull requests
  as conventional commits.
- Security issues: [SECURITY.md](SECURITY.md).

Apache-2.0, © NimbusXR.
