@echo off
call build.bat
if %errorlevel% neq 0 exit /b %errorlevel%
.build\hyperax.exe serve --log=.build\run.log
