# https://github.com/reteps/dockerfmt/issues/72
FROM mcr.microsoft.com/powershell
SHELL ["pwsh", "-Command"]
RUN if ($true) { Write-Host hi }
RUN Get-ChildItem -Path C:\ | Select-Object Name
CMD Write-Host   a
FROM alpine
RUN echo   reset
FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["cmd", "/S", "/C"]
RUN dir   C:\ && echo   done
SHELL ["powershell.exe", "-Command"]
ONBUILD RUN Write-Host   onbuild
SHELL ["/bin/bash", "-c"]
RUN echo   bash   again
