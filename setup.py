from distutils.core import setup

setup(
    name='valet-sh-cli',
    version='1.0',
    package_dir={'/usr/share/valet-sh'},
    license='Creative Commons Attribution-Noncommercial-Share Alike license',
    long_description=open('README.md').read(),
)