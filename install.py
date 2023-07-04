import json
import locale
import os
import sys
import threading
import subprocess

import googletrans
from PyQt6 import QtWidgets
from PyQt6.QtGui import QIcon
from dulwich import porcelain
from dulwich.repo import Repo
from dulwich.client import get_transport_and_path
from PyQt6.QtWidgets import QApplication, QMessageBox
from PyQt6.QtCore import pyqtSignal, QObject

repoData = json.load(open("repo.json"))

colors_dict = {
    "primary_color":"#1A1D22",
    "secondary_color":"#282C34",
    "hover_color":"#596273",
    "text_color":"#FFFFFF",
    "toggle_color":"#4a708b",
    "green":"#3a7a3a",
    "yellow":"#7a7a3a",
    "red":"#7a3a3a"
}

translator = googletrans.Translator()

class SignalEmitter(QObject):
    signal = pyqtSignal()

class BoolSignalEmitter(QObject):
    signal = pyqtSignal(bool)
def translate_ui_text(text):
    if text is None or text == "":
        return text

    langCode = locale.getdefaultlocale()[0].split("_")[0]

    counter = 0
    translatedText = None
    while counter < 10:
        try:
            if "en" in langCode.lower():
                translatedText = text
            else:
                translatedText = translator.translate(text, dest=langCode).text
            break
        except TypeError:
            counter += 1
        except Exception:
            print("Timeout error when trying to use google translate. Not going to translate.")
            break

    if translatedText is None:
        print("Failed to get translation. Not translating.")
        translatedText = text
        translatedText = translatedText[0].upper() + translatedText[1:]

    if langCode not in ['ja', 'zh-cn', 'zh-tw']:  # Add more if needed
        translatedText = translatedText[0].upper() + translatedText[1:]

    translatedText = translatedText.strip()

    return translatedText

normalInstallText = translate_ui_text("Updating packages...")
torchInstallText = translate_ui_text("Updating pytorch, this may take a while...")

def get_stylesheet():
    styleSheet = """
    * {
        background-color: {primary_color};
        color: {secondary_color};
    }
    
    QLabel {
        color: {text_color};
    }
    
    QMessageBox {
        background-color: {primary_color};
        color: {text_color};
    }
    
    QProgressBar {
            border: 0px solid {hover_color};
            text-align: center;
            background-color: {secondary_color};
            color: {text_color};
    }
    QProgressBar::chunk {
        background-color: {toggle_color};
    }
    """

    for colorKey, colorValue in colors_dict.items():
        styleSheet = styleSheet.replace("{" + colorKey + "}", colorValue)
    return styleSheet


class DownloadDialog(QtWidgets.QDialog):
    def __init__(self, packages):
        super().__init__()
        self.setWindowTitle(translate_ui_text('Download Progress'))
        self.packages = packages
        self.signalEmitter = SignalEmitter()
        self.signalEmitter.signal.connect(lambda: self.done(0))

        self.boolSignalEmitter = BoolSignalEmitter()
        self.boolSignalEmitter.signal.connect(self.setpytorch)

        self.layout = QtWidgets.QVBoxLayout()
        self.label = QtWidgets.QLabel(normalInstallText)
        self.layout.addWidget(self.label)

        self.progress = QtWidgets.QProgressBar(self)
        self.layout.addWidget(self.progress)

        self.setLayout(self.layout)

        self.download_thread = threading.Thread(target=self.install_packages)

    def install_packages(self):
        total_packages = len(self.packages)
        self.progress.setMaximum(100)

        for i, package in enumerate(self.packages):
            package:str
            self.boolSignalEmitter.signal.emit(package.startswith("-r"))

            try:
                if package.startswith("-r"):
                    subprocess.run([sys.executable, '-m', 'pip', 'install', '--upgrade', "-r", package[2:].strip()], check=True, text=True, capture_output=True)
                else:
                    subprocess.run([sys.executable, '-m', 'pip', 'install', '--upgrade', package], check=True, text=True, capture_output=True)
            except subprocess.CalledProcessError as e:
                QMessageBox.critical(self, 'Error', f"An error occurred while installing package '{package}':\n{e.stderr}")
                return

            self.update_progress_bar(i + 1, total_packages)

        self.signalEmitter.signal.emit()

    def setpytorch(self, isPytorch:bool):
        newText = torchInstallText if isPytorch else normalInstallText
        if self.label.text() != newText:
            self.label.setText(newText)


    def finish(self):
        self.done(0)

    def update_progress_bar(self, progress_tracker, total_size_in_bytes):
        percent_completed = (progress_tracker / total_size_in_bytes) * 100
        self.progress.setValue(int(percent_completed))

    def exec(self):
        self.download_thread.start()
        super().exec()

    def show(self):
        self.download_thread.start()
        super().show()

def clone_or_pull(gitUrl, targetDirectory):
    if not os.path.exists(targetDirectory):
        porcelain.clone(gitUrl, target=targetDirectory)
    else:
        porcelain.pull(targetDirectory, gitUrl)

def run_startup(repo_dir, script):
    if os.path.exists(os.path.join(repo_dir, script)):
        os.chdir(repo_dir)
        subprocess.check_call([sys.executable, script])


def check_requirements(repo_dir):
    req_file = os.path.join(repo_dir, 'requirements.txt')
    packages = []
    torch_req_file = os.path.join(repo_dir, 'requirements-torch.txt')
    if os.path.exists(torch_req_file):
        packages.append('-r ' + torch_req_file)

    if os.path.exists(req_file):
        with open(req_file, 'r') as f:
            lines = f.read().splitlines()

        # Strip out comments and whitespace, ignore empty lines
        packages += [line.split('#', 1)[0].strip() for line in lines if line.split('#', 1)[0].strip()]



    return packages


def check_if_latest(repo_path, remote_url) -> bool:
    # Open the local repository
    repo = Repo(repo_path)

    # Get the current commit
    head = repo[b"HEAD"]

    # Open the remote repository
    client, path = get_transport_and_path(remote_url)
    remote_refs = client.get_refs(path)

    # Check if current commit is the latest one
    return head.id == remote_refs[b"HEAD"]

def main():
    app = QApplication([])
    if "icon" in repoData:
        app.setWindowIcon(QIcon(repoData["icon"]))
    app.setStyleSheet(get_stylesheet())
    repoURL = repoData["repo_url"]
    repoDir = repoData["repo_dir"]
    startupScript = repoData["startup_script"]

    #If it's missing or not the latest commit anymore, do a pull and make sure the requirements haven't changed.
    #Also if it was previously installing and was interrupted partway through.
    if os.path.exists("installing") or not os.path.exists(repoDir) or not check_if_latest(repoDir, repoURL):
        open("installing", 'w').close()
        clone_or_pull(repoURL,repoDir)
        packages = check_requirements(repoDir)

        if len(packages) > 0:
            dialog = DownloadDialog(packages)
            dialog.exec()
        os.remove("installing")


    run_startup(repoDir, startupScript)
    sys.exit()

if __name__ == "__main__":
    if os.name == "nt":
        import ctypes
        myappid = u'lugia19.installer'
        ctypes.windll.shell32.SetCurrentProcessExplicitAppUserModelID(myappid)
    main()
