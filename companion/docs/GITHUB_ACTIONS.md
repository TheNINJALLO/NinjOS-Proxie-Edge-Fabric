# GitHub upload and compile guide

## 1. Create the repository

Create a new GitHub repository. A private repository is recommended because
this plugin is part of the private Ninj-OS server infrastructure.

Do not initialize it with another README, `.gitignore`, or license when using
the supplied repository package.

## 2. Upload the package

Upload every file and folder from this package to the repository root.

The following path must remain exact:

```text
.github/workflows/build-companion.yml
```

Do not upload only the `src` folder. The workflow, `CMakeLists.txt`, example
configuration, and documentation all belong at the repository root.

## 3. Enable GitHub Actions

Open:

```text
Repository
→ Actions
```

If GitHub asks whether to enable workflows, enable them.

For a restricted private repository, also check:

```text
Repository
→ Settings
→ Actions
→ General
```

Allow GitHub Actions to run the actions referenced by the supplied workflow.

## 4. Run the compiler

Open:

```text
Actions
→ Build Endstone Companion
→ Run workflow
```

Enter the exact Endstone release without the leading `v`.

Example:

```text
0.11.6
```

Then choose **Run workflow**.

## 5. Download the result

When the workflow finishes successfully:

```text
Actions
→ completed workflow run
→ Artifacts
```

Download:

```text
NinjOS-Endstone-Companion-Endstone-<version>
```

The artifact contains a ZIP named like:

```text
NinjOS-Endstone-Companion-Linux-x86_64-Endstone-0.11.6.zip
```

## 6. When compilation fails

Open the failed step and copy the compiler output. Repair only the reported
Endstone API compatibility problems, keep the plugin in C++, and rerun the
workflow. Do not remove packet events merely to make the build pass.
