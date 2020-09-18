from distutils.core import setup

setup(
    name='valet-sh-cli',
    author='Johann Zelger',
    author_email='j.zelger@techdivision.com',
    description='valet.sh command line interface written in bash',
    url='https://github.com/valet-sh/cli',
    maintainer='Johann Zelger',
    maintainer_email='j.zelger@techdivision.com',
    version='1.2.3',
    license='Creative Commons Attribution-Noncommercial-Share Alike license',
    long_description=open('README.md').read(),
    scripts=['valet.sh']
)
