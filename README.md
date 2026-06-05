# Go+Python installer/updater

Essentially, this is just an installer designed to pull a project from a specified github repo, and install its prerequisites in a venv (and keep it up to date with the repo).

The components that are required to ship an application using it are:
- The Go executable (exe for windows for example)
- The uv binary (uv.exe for windows) — used to download Python, create venvs, and install packages
- The install.py script
- A repo.json file containing the settings (such as the github repository, python version, etc)

The rundown on its functionality is:

- Go program:
  - Functions as the entrypoint (gets called to actually start the program)
  - Uses uv to download a Python runtime and create a new venv if not already done
  - Installs base requirements (PyQt6, dulwich, etc.) via uv into the venv
  - Calls install.py from the new venv
- Install.py:
  - Uses dulwich to clone/pull the specified github repo
  - Installs the requirements from the repo's requirements.txt via uv (or try to update them if the repo has been updated)
    - First install packages from requirements-torch.txt if present. This is designed to allow you to install pytorch with CUDA easily, with a GUI download progress dialog for large wheels.
  - Launches the script defined in repo.json to start the application itself

## repo.json fields

- `repo_url`: GitHub repository URL to clone/pull
- `repo_dir`: Local directory name for the cloned repo
- `startup_script`: Python script to run from the repo (e.g. `main.py`)
- `use_pythonw`: Use `pythonw.exe` instead of `python.exe` (hides console for GUI apps)
- `venv_folder`: Virtual environment folder name (default: `venv`)
- `python_version`: Python version to install via uv (e.g. `3.11`)
- `icon`: Optional path to an application icon (.ico)
