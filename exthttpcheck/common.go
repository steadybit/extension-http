// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2024 Steadybit GmbH

package exthttpcheck

import (
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
)

const (
	targetType = "com.steadybit.extension_http.client-location"
	targetIcon = "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjQiIGhlaWdodD0iMjQiIHZpZXdCb3g9IjAgMCAyNCAyNCIgZmlsbD0ibm9uZSIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj4KPHBhdGggZmlsbC1ydWxlPSJldmVub2RkIiBjbGlwLXJ1bGU9ImV2ZW5vZGQiIGQ9Ik00LjUgOS4xQzQuNSA0LjkyIDcuODc1IDEuNSAxMiAxLjVDMTYuMTI1IDEuNSAxOS41IDQuOTIgMTkuNSA5LjFDMTkuNSAxMy45MTM1IDEzLjcyMjQgMTkuMzEyNCAxMi43NzcgMjAuMTk1OEMxMi43MTQ5IDIwLjI1MzkgMTIuNjczNiAyMC4yOTI0IDEyLjY1NjIgMjAuMzFDMTIuNDY4OCAyMC40MDUgMTIuMTg3NSAyMC41IDEyIDIwLjVDMTEuODEyNSAyMC41IDExLjUzMTIgMjAuNDA1IDExLjM0MzggMjAuMzFDMTEuMzI2NCAyMC4yOTI0IDExLjI4NTEgMjAuMjUzOSAxMS4yMjMgMjAuMTk1OEMxMC4yNzc2IDE5LjMxMjQgNC41IDEzLjkxMzUgNC41IDkuMVpNNi4zNzUgOS4xQzYuMzc1IDEyLjMzIDEwLjAzMTIgMTYuNDE1IDEyIDE4LjMxNUMxMy45Njg4IDE2LjQxNSAxNy42MjUgMTIuMjM1IDE3LjYyNSA5LjFDMTcuNjI1IDUuOTY1IDE1LjA5MzggMy40IDEyIDMuNEM4LjkwNjI1IDMuNCA2LjM3NSA1Ljk2NSA2LjM3NSA5LjFaTTguMjUgOS4xQzguMjUgNy4wMSA5LjkzNzUgNS4zIDEyIDUuM0MxNC4wNjI1IDUuMyAxNS43NSA3LjAxIDE1Ljc1IDkuMUMxNS43NSAxMS4xOSAxNC4wNjI1IDEyLjkgMTIgMTIuOUM5LjkzNzUgMTIuOSA4LjI1IDExLjE5IDguMjUgOS4xWk0xMC4xMjUgOS4xQzEwLjEyNSAxMC4xNDUgMTAuOTY4OCAxMSAxMiAxMUMxMy4wMzEyIDExIDEzLjg3NSAxMC4xNDUgMTMuODc1IDkuMUMxMy44NzUgOC4wNTUgMTMuMDMxMiA3LjIgMTIgNy4yQzEwLjk2ODggNy4yIDEwLjEyNSA4LjA1NSAxMC4xMjUgOS4xWk01LjA3MzMyIDE2Ljg3NDVDNS41NTYyNyAxNi42MDY2IDUuNzMwNjEgMTUuOTk3OSA1LjQ2MjcgMTUuNTE0OUM1LjE5NDggMTUuMDMyIDQuNTg2MSAxNC44NTc2IDQuMTAzMTUgMTUuMTI1NUMyLjc0Mzg0IDE1Ljg3OTYgMiAxNi45NTE1IDIgMTguMjVDMiAxOS4xMTYxIDIuNDI1NTEgMTkuODUwNyAzLjAwNTQ4IDIwLjQyMjFDMy41ODE5MyAyMC45ODk5IDQuMzY0NzYgMjEuNDU1MyA1LjI1MTQyIDIxLjgyNDdDNy4wMjkxMyAyMi41NjU0IDkuNDE1NjkgMjMgMTIgMjNDMTQuNTg0MyAyMyAxNi45NzA5IDIyLjU2NTQgMTguNzQ4NiAyMS44MjQ3QzE5LjYzNTIgMjEuNDU1MyAyMC40MTgxIDIwLjk4OTkgMjAuOTk0NSAyMC40MjIxQzIxLjU3NDUgMTkuODUwNyAyMiAxOS4xMTYxIDIyIDE4LjI1QzIyIDE2Ljk1MTUgMjEuMjU2MiAxNS44Nzk2IDE5Ljg5NjkgMTUuMTI1NUMxOS40MTM5IDE0Ljg1NzYgMTguODA1MiAxNS4wMzIgMTguNTM3MyAxNS41MTQ5QzE4LjI2OTQgMTUuOTk3OSAxOC40NDM3IDE2LjYwNjYgMTguOTI2NyAxNi44NzQ1QzE5LjgyNzEgMTcuMzczOSAyMCAxNy44NjAxIDIwIDE4LjI1QzIwIDE4LjQxOTQgMTkuOTIxOCAxOC42NzEzIDE5LjU5MSAxOC45OTczQzE5LjI1NjUgMTkuMzI2NyAxOC43MjE0IDE5LjY2OTQgMTcuOTc5MyAxOS45Nzg2QzE2LjQ5OTcgMjAuNTk1MSAxNC4zODYzIDIxIDEyIDIxQzkuNjEzNzUgMjEgNy41MDAzMSAyMC41OTUxIDYuMDIwNjUgMTkuOTc4NkM1LjI3ODY0IDE5LjY2OTQgNC43NDM0NSAxOS4zMjY3IDQuNDA5MDUgMTguOTk3M0M0LjA3ODE3IDE4LjY3MTMgNCAxOC40MTk0IDQgMTguMjVDNCAxNy44NjAxIDQuMTcyOTUgMTcuMzczOSA1LjA3MzMyIDE2Ljg3NDVaIiBmaWxsPSIjMUQyNjMyIi8+Cjwvc3ZnPgo="

	ActionIDPeriodically = "com.steadybit.extension_http.check.periodically"
	ActionIDFixedAmount  = "com.steadybit.extension_http.check.fixed_amount"

	actionIconPeriodically = "data:image/svg+xml,%3Csvg%20width%3D%2224%22%20height%3D%2224%22%20viewBox%3D%220%200%2024%2024%22%20fill%3D%22none%22%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%3E%0A%3Cpath%20d%3D%22M4.38098%2013C4.17057%2013%204%2012.8294%204%2012.619V9.38098C4%209.17057%204.17057%209%204.38098%209V9C4.59139%209%204.76196%209.17057%204.76196%209.38098V10.6934H6.71103V9.38201C6.71103%209.17103%206.88206%209%207.09304%209V9C7.30402%209%207.47505%209.17103%207.47505%209.38201V12.618C7.47505%2012.829%207.30402%2013%207.09304%2013V13C6.88206%2013%206.71103%2012.829%206.71103%2012.618V11.3008H4.76196V12.619C4.76196%2012.8294%204.59139%2013%204.38098%2013V13Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M8.42263%209.60742C8.25489%209.60742%208.11892%209.47145%208.11892%209.30371V9.30371C8.11892%209.13598%208.25489%209%208.42263%209H11.1711C11.3389%209%2011.4748%209.13598%2011.4748%209.30371V9.30371C11.4748%209.47145%2011.3389%209.60742%2011.1711%209.60742H10.1748V12.6221C10.1748%2012.8308%2010.0056%2013%209.79688%2013V13C9.58817%2013%209.41898%2012.8308%209.41898%2012.6221V9.60742H8.42263Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M12.2407%209.60742C12.0729%209.60742%2011.9369%209.47145%2011.9369%209.30371V9.30371C11.9369%209.13598%2012.0729%209%2012.2407%209H14.9892C15.1569%209%2015.2929%209.13598%2015.2929%209.30371V9.30371C15.2929%209.47145%2015.1569%209.60742%2014.9892%209.60742H13.9928V12.6221C13.9928%2012.8308%2013.8236%2013%2013.6149%2013V13C13.4062%2013%2013.237%2012.8308%2013.237%2012.6221V9.60742H12.2407Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M16.3208%2013C16.1104%2013%2015.9398%2012.8294%2015.9398%2012.619V9.5C15.9398%209.22386%2016.1637%209%2016.4398%209H17.5171C17.8403%209%2018.1114%209.05729%2018.3305%209.17187C18.5509%209.28646%2018.7173%209.44401%2018.8295%209.64453C18.9432%209.84375%2019%2010.0703%2019%2010.3242C19%2010.5807%2018.9432%2010.8086%2018.8295%2011.0078C18.7159%2011.207%2018.5482%2011.3639%2018.3263%2011.4785C18.1045%2011.5918%2017.8314%2011.6484%2017.5069%2011.6484H16.4615V11.0527H17.4042C17.5931%2011.0527%2017.7479%2011.0215%2017.8684%2010.959C17.9888%2010.8965%2018.0778%2010.8105%2018.1353%2010.7012C18.1942%2010.5918%2018.2237%2010.4661%2018.2237%2010.3242C18.2237%2010.1823%2018.1942%2010.0573%2018.1353%209.94922C18.0778%209.84115%2017.9882%209.75716%2017.8663%209.69727C17.7458%209.63607%2017.5904%209.60547%2017.4001%209.60547H16.7018V12.619C16.7018%2012.8294%2016.5312%2013%2016.3208%2013V13Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M4.38098%2013C4.17057%2013%204%2012.8294%204%2012.619V9.38098C4%209.17057%204.17057%209%204.38098%209V9C4.59139%209%204.76196%209.17057%204.76196%209.38098V10.6934H6.71103V9.38201C6.71103%209.17103%206.88206%209%207.09304%209V9C7.30402%209%207.47505%209.17103%207.47505%209.38201V12.618C7.47505%2012.829%207.30402%2013%207.09304%2013V13C6.88206%2013%206.71103%2012.829%206.71103%2012.618V11.3008H4.76196V12.619C4.76196%2012.8294%204.59139%2013%204.38098%2013V13Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Cpath%20d%3D%22M8.42263%209.60742C8.25489%209.60742%208.11892%209.47145%208.11892%209.30371V9.30371C8.11892%209.13598%208.25489%209%208.42263%209H11.1711C11.3389%209%2011.4748%209.13598%2011.4748%209.30371V9.30371C11.4748%209.47145%2011.3389%209.60742%2011.1711%209.60742H10.1748V12.6221C10.1748%2012.8308%2010.0056%2013%209.79688%2013V13C9.58817%2013%209.41898%2012.8308%209.41898%2012.6221V9.60742H8.42263Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Cpath%20d%3D%22M12.2407%209.60742C12.0729%209.60742%2011.9369%209.47145%2011.9369%209.30371V9.30371C11.9369%209.13598%2012.0729%209%2012.2407%209H14.9892C15.1569%209%2015.2929%209.13598%2015.2929%209.30371V9.30371C15.2929%209.47145%2015.1569%209.60742%2014.9892%209.60742H13.9928V12.6221C13.9928%2012.8308%2013.8236%2013%2013.6149%2013V13C13.4062%2013%2013.237%2012.8308%2013.237%2012.6221V9.60742H12.2407Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Cpath%20d%3D%22M16.3208%2013C16.1104%2013%2015.9398%2012.8294%2015.9398%2012.619V9.5C15.9398%209.22386%2016.1637%209%2016.4398%209H17.5171C17.8403%209%2018.1114%209.05729%2018.3305%209.17187C18.5509%209.28646%2018.7173%209.44401%2018.8295%209.64453C18.9432%209.84375%2019%2010.0703%2019%2010.3242C19%2010.5807%2018.9432%2010.8086%2018.8295%2011.0078C18.7159%2011.207%2018.5482%2011.3639%2018.3263%2011.4785C18.1045%2011.5918%2017.8314%2011.6484%2017.5069%2011.6484H16.4615V11.0527H17.4042C17.5931%2011.0527%2017.7479%2011.0215%2017.8684%2010.959C17.9888%2010.8965%2018.0778%2010.8105%2018.1353%2010.7012C18.1942%2010.5918%2018.2237%2010.4661%2018.2237%2010.3242C18.2237%2010.1823%2018.1942%2010.0573%2018.1353%209.94922C18.0778%209.84115%2017.9882%209.75716%2017.8663%209.69727C17.7458%209.63607%2017.5904%209.60547%2017.4001%209.60547H16.7018V12.619C16.7018%2012.8294%2016.5312%2013%2016.3208%2013V13Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Ccircle%20cx%3D%224.5%22%20cy%3D%224.5%22%20r%3D%220.5%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Ccircle%20cx%3D%226.5%22%20cy%3D%224.5%22%20r%3D%220.5%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Ccircle%20cx%3D%228.5%22%20cy%3D%224.5%22%20r%3D%220.5%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M10.5%2022H4C2.89543%2022%202%2021.1046%202%2020V7M22%2012V7M2%207V4C2%202.89543%202.89543%202%204%202H20C21.1046%202%2022%202.89543%2022%204V7M2%207H22%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%221.6%22%20stroke-linecap%3D%22round%22%2F%3E%0A%3Cpath%20fill-rule%3D%22evenodd%22%20clip-rule%3D%22evenodd%22%20d%3D%22M18.5006%2015C16.5673%2015%2015.0001%2016.567%2015.0001%2018.5C15.0001%2020.433%2016.5673%2022%2018.5006%2022C20.4338%2022%2022.001%2020.433%2022.001%2018.5C22.001%2016.567%2020.4338%2015%2018.5006%2015ZM14%2018.5C14%2016.0147%2016.015%2014%2018.5006%2014C20.9862%2014%2023.0011%2016.0147%2023.0011%2018.5C23.0011%2020.9853%2020.9862%2023%2018.5006%2023C16.015%2023%2014%2020.9853%2014%2018.5Z%22%20fill%3D%22%231D2632%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.2%22%20stroke-linecap%3D%22round%22%20stroke-linejoin%3D%22round%22%2F%3E%0A%3Cpath%20fill-rule%3D%22evenodd%22%20clip-rule%3D%22evenodd%22%20d%3D%22M18.4516%2017C18.701%2017%2018.9032%2017.2022%2018.9032%2017.4516V18.5962H20.0484C20.2978%2018.5962%2020.5%2018.7984%2020.5%2019.0478C20.5%2019.2972%2020.2978%2019.4994%2020.0484%2019.4994H18.4516C18.2022%2019.4994%2018%2019.2972%2018%2019.0478V17.4516C18%2017.2022%2018.2022%2017%2018.4516%2017Z%22%20fill%3D%22%231D2632%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.2%22%20stroke-linecap%3D%22round%22%20stroke-linejoin%3D%22round%22%2F%3E%0A%3C%2Fsvg%3E%0A"
	actionIconFixedAmount  = "data:image/svg+xml,%3Csvg%20width%3D%2224%22%20height%3D%2224%22%20viewBox%3D%220%200%2024%2024%22%20fill%3D%22none%22%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%3E%0A%3Cpath%20d%3D%22M4.38098%2013C4.17057%2013%204%2012.8294%204%2012.619V9.38098C4%209.17057%204.17057%209%204.38098%209V9C4.59139%209%204.76196%209.17057%204.76196%209.38098V10.6934H6.71103V9.38201C6.71103%209.17103%206.88206%209%207.09304%209V9C7.30402%209%207.47505%209.17103%207.47505%209.38201V12.618C7.47505%2012.829%207.30402%2013%207.09304%2013V13C6.88206%2013%206.71103%2012.829%206.71103%2012.618V11.3008H4.76196V12.619C4.76196%2012.8294%204.59139%2013%204.38098%2013V13Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M8.42263%209.60742C8.25489%209.60742%208.11892%209.47145%208.11892%209.30371V9.30371C8.11892%209.13598%208.25489%209%208.42263%209H11.1711C11.3389%209%2011.4748%209.13598%2011.4748%209.30371V9.30371C11.4748%209.47145%2011.3389%209.60742%2011.1711%209.60742H10.1748V12.6221C10.1748%2012.8308%2010.0056%2013%209.79688%2013V13C9.58817%2013%209.41898%2012.8308%209.41898%2012.6221V9.60742H8.42263Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M12.2407%209.60742C12.0729%209.60742%2011.9369%209.47145%2011.9369%209.30371V9.30371C11.9369%209.13598%2012.0729%209%2012.2407%209H14.9892C15.1569%209%2015.2929%209.13598%2015.2929%209.30371V9.30371C15.2929%209.47145%2015.1569%209.60742%2014.9892%209.60742H13.9928V12.6221C13.9928%2012.8308%2013.8236%2013%2013.6149%2013V13C13.4062%2013%2013.237%2012.8308%2013.237%2012.6221V9.60742H12.2407Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M16.3208%2013C16.1104%2013%2015.9398%2012.8294%2015.9398%2012.619V9.5C15.9398%209.22386%2016.1637%209%2016.4398%209H17.5171C17.8403%209%2018.1114%209.05729%2018.3305%209.17187C18.5509%209.28646%2018.7173%209.44401%2018.8295%209.64453C18.9432%209.84375%2019%2010.0703%2019%2010.3242C19%2010.5807%2018.9432%2010.8086%2018.8295%2011.0078C18.7159%2011.207%2018.5482%2011.3639%2018.3263%2011.4785C18.1045%2011.5918%2017.8314%2011.6484%2017.5069%2011.6484H16.4615V11.0527H17.4042C17.5931%2011.0527%2017.7479%2011.0215%2017.8684%2010.959C17.9888%2010.8965%2018.0778%2010.8105%2018.1353%2010.7012C18.1942%2010.5918%2018.2237%2010.4661%2018.2237%2010.3242C18.2237%2010.1823%2018.1942%2010.0573%2018.1353%209.94922C18.0778%209.84115%2017.9882%209.75716%2017.8663%209.69727C17.7458%209.63607%2017.5904%209.60547%2017.4001%209.60547H16.7018V12.619C16.7018%2012.8294%2016.5312%2013%2016.3208%2013V13Z%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M4.38098%2013C4.17057%2013%204%2012.8294%204%2012.619V9.38098C4%209.17057%204.17057%209%204.38098%209V9C4.59139%209%204.76196%209.17057%204.76196%209.38098V10.6934H6.71103V9.38201C6.71103%209.17103%206.88206%209%207.09304%209V9C7.30402%209%207.47505%209.17103%207.47505%209.38201V12.618C7.47505%2012.829%207.30402%2013%207.09304%2013V13C6.88206%2013%206.71103%2012.829%206.71103%2012.618V11.3008H4.76196V12.619C4.76196%2012.8294%204.59139%2013%204.38098%2013V13Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Cpath%20d%3D%22M8.42263%209.60742C8.25489%209.60742%208.11892%209.47145%208.11892%209.30371V9.30371C8.11892%209.13598%208.25489%209%208.42263%209H11.1711C11.3389%209%2011.4748%209.13598%2011.4748%209.30371V9.30371C11.4748%209.47145%2011.3389%209.60742%2011.1711%209.60742H10.1748V12.6221C10.1748%2012.8308%2010.0056%2013%209.79688%2013V13C9.58817%2013%209.41898%2012.8308%209.41898%2012.6221V9.60742H8.42263Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Cpath%20d%3D%22M12.2407%209.60742C12.0729%209.60742%2011.9369%209.47145%2011.9369%209.30371V9.30371C11.9369%209.13598%2012.0729%209%2012.2407%209H14.9892C15.1569%209%2015.2929%209.13598%2015.2929%209.30371V9.30371C15.2929%209.47145%2015.1569%209.60742%2014.9892%209.60742H13.9928V12.6221C13.9928%2012.8308%2013.8236%2013%2013.6149%2013V13C13.4062%2013%2013.237%2012.8308%2013.237%2012.6221V9.60742H12.2407Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Cpath%20d%3D%22M16.3208%2013C16.1104%2013%2015.9398%2012.8294%2015.9398%2012.619V9.5C15.9398%209.22386%2016.1637%209%2016.4398%209H17.5171C17.8403%209%2018.1114%209.05729%2018.3305%209.17187C18.5509%209.28646%2018.7173%209.44401%2018.8295%209.64453C18.9432%209.84375%2019%2010.0703%2019%2010.3242C19%2010.5807%2018.9432%2010.8086%2018.8295%2011.0078C18.7159%2011.207%2018.5482%2011.3639%2018.3263%2011.4785C18.1045%2011.5918%2017.8314%2011.6484%2017.5069%2011.6484H16.4615V11.0527H17.4042C17.5931%2011.0527%2017.7479%2011.0215%2017.8684%2010.959C17.9888%2010.8965%2018.0778%2010.8105%2018.1353%2010.7012C18.1942%2010.5918%2018.2237%2010.4661%2018.2237%2010.3242C18.2237%2010.1823%2018.1942%2010.0573%2018.1353%209.94922C18.0778%209.84115%2017.9882%209.75716%2017.8663%209.69727C17.7458%209.63607%2017.5904%209.60547%2017.4001%209.60547H16.7018V12.619C16.7018%2012.8294%2016.5312%2013%2016.3208%2013V13Z%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.3%22%2F%3E%0A%3Ccircle%20cx%3D%224.5%22%20cy%3D%224.5%22%20r%3D%220.5%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Ccircle%20cx%3D%226.5%22%20cy%3D%224.5%22%20r%3D%220.5%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Ccircle%20cx%3D%228.5%22%20cy%3D%224.5%22%20r%3D%220.5%22%20fill%3D%22%231D2632%22%2F%3E%0A%3Cpath%20d%3D%22M10%2022H4C2.89543%2022%202%2021.1046%202%2020V7M22%2012V7M2%207V4C2%202.89543%202.89543%202%204%202H20C21.1046%202%2022%202.89543%2022%204V7M2%207H22%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%221.6%22%20stroke-linecap%3D%22round%22%2F%3E%0A%3Cpath%20fill-rule%3D%22evenodd%22%20clip-rule%3D%22evenodd%22%20d%3D%22M18.5006%2015C16.5673%2015%2015.0001%2016.567%2015.0001%2018.5C15.0001%2020.433%2016.5673%2022%2018.5006%2022C20.4338%2022%2022.001%2020.433%2022.001%2018.5C22.001%2016.567%2020.4338%2015%2018.5006%2015ZM14%2018.5C14%2016.0147%2016.015%2014%2018.5006%2014C20.9862%2014%2023.0011%2016.0147%2023.0011%2018.5C23.0011%2020.9853%2020.9862%2023%2018.5006%2023C16.015%2023%2014%2020.9853%2014%2018.5Z%22%20fill%3D%22%231D2632%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.2%22%20stroke-linecap%3D%22round%22%20stroke-linejoin%3D%22round%22%2F%3E%0A%3Cpath%20d%3D%22M18.6508%2021L19.6157%2016H20.4199L19.4549%2021H18.6508ZM16%2019.7109L16.135%2019.0273H20.6611L20.5261%2019.7109H16ZM16.583%2021L17.548%2016H18.3521L17.3871%2021H16.583ZM16.3418%2017.9727L16.4739%2017.2891H21L20.8679%2017.9727H16.3418Z%22%20fill%3D%22%231D2632%22%20stroke%3D%22%231D2632%22%20stroke-width%3D%220.2%22%20stroke-linecap%3D%22round%22%20stroke-linejoin%3D%22round%22%2F%3E%0A%3C%2Fsvg%3E%0A"
)

var (
	targetSelection = extutil.Ptr(action_kit_api.TargetSelection{
		TargetType: targetType,
		DefaultBlastRadius: extutil.Ptr(action_kit_api.DefaultBlastRadius{
			Mode:  action_kit_api.DefaultBlastRadiusModeMaximum,
			Value: 1,
		}),
		MissingQuerySelection: extutil.Ptr(action_kit_api.MissingQuerySelectionIncludeAll),
	})

	requestDefinition = action_kit_api.ActionParameter{
		Name:  "requestDefinition",
		Label: "Request Definition",
		Type:  action_kit_api.ActionParameterTypeHeader,
		Order: extutil.Ptr(0),
	}
	method = action_kit_api.ActionParameter{
		Name:         "method",
		Label:        "HTTP Method",
		Description:  extutil.Ptr("The HTTP method to use."),
		Type:         action_kit_api.ActionParameterTypeString,
		DefaultValue: extutil.Ptr("GET"),
		Required:     extutil.Ptr(true),
		Order:        extutil.Ptr(1),
		Options: extutil.Ptr([]action_kit_api.ParameterOption{
			action_kit_api.ExplicitParameterOption{
				Label: "GET",
				Value: "GET",
			},
			action_kit_api.ExplicitParameterOption{
				Label: "POST",
				Value: "POST",
			},
			action_kit_api.ExplicitParameterOption{
				Label: "PUT",
				Value: "PUT",
			},
			action_kit_api.ExplicitParameterOption{
				Label: "PATCH",
				Value: "PATCH",
			},
			action_kit_api.ExplicitParameterOption{
				Label: "HEAD",
				Value: "HEAD",
			},
			action_kit_api.ExplicitParameterOption{
				Label: "DELETE",
				Value: "DELETE",
			},
		}),
	}
	urlParameter = action_kit_api.ActionParameter{
		Name:        "url",
		Label:       "Target URL",
		Description: extutil.Ptr("The URL to check."),
		Type:        action_kit_api.ActionParameterTypeUrl,
		Required:    extutil.Ptr(true),
		Order:       extutil.Ptr(2),
	}
	body = action_kit_api.ActionParameter{
		Name:        "body",
		Label:       "HTTP Body",
		Description: extutil.Ptr("The HTTP Body."),
		Type:        action_kit_api.ActionParameterTypeTextarea,
		Order:       extutil.Ptr(3),
	}
	headers = action_kit_api.ActionParameter{
		Name:        "headers",
		Label:       "HTTP Headers",
		Description: extutil.Ptr("The HTTP Headers."),
		Type:        action_kit_api.ActionParameterTypeKeyValue,
		Order:       extutil.Ptr(4),
	}
	repetitionControl = action_kit_api.ActionParameter{
		Name:  "repetitionControl",
		Label: "Repetition Control",
		Type:  action_kit_api.ActionParameterTypeHeader,
		Order: extutil.Ptr(6),
	}
	duration = action_kit_api.ActionParameter{
		Name:         "duration",
		Label:        "Duration",
		Description:  extutil.Ptr("In which timeframe should the specified requests be executed?"),
		Type:         action_kit_api.ActionParameterTypeDuration,
		DefaultValue: extutil.Ptr("10s"),
		Required:     extutil.Ptr(true),
		Order:        extutil.Ptr(8),
	}
	resultVerification = action_kit_api.ActionParameter{
		Name:  "resultVerification",
		Label: "Result Verification",
		Type:  action_kit_api.ActionParameterTypeHeader,
		Order: extutil.Ptr(10),
	}
	successRate = action_kit_api.ActionParameter{
		Name:         "successRate",
		Label:        "Required Success Rate",
		Description:  extutil.Ptr("How many percent of all requests must be at least successful (according to response verifications)? The result will be evaluated at the end of the given duration."),
		Type:         action_kit_api.ActionParameterTypePercentage,
		DefaultValue: extutil.Ptr("100"),
		Required:     extutil.Ptr(true),
		Order:        extutil.Ptr(11),
		MinValue:     extutil.Ptr(0),
		MaxValue:     extutil.Ptr(100),
	}
	statusCode = action_kit_api.ActionParameter{
		Name:         "statusCode",
		Label:        "Response status codes",
		Description:  extutil.Ptr("Which HTTP-Status codes should be considered as success? This field supports ranges with '-' and multiple codes delimited by ';' for example '200-399;429'."),
		Type:         action_kit_api.ActionParameterTypeString,
		DefaultValue: extutil.Ptr("200-299"),
		Required:     extutil.Ptr(true),
		Order:        extutil.Ptr(12),
	}
	responsesContains = action_kit_api.ActionParameter{
		Name:        "responsesContains",
		Label:       "Responses contains",
		Description: extutil.Ptr("The responses must contain the given string, otherwise the step will fail."),
		Type:        action_kit_api.ActionParameterTypeString,
		Required:    extutil.Ptr(false),
		Order:       extutil.Ptr(13),
	}
	responseTimeMode = action_kit_api.ActionParameter{
		Name:         "responseTimeMode",
		Label:        "Response Time Verification Mode",
		Description:  extutil.Ptr("Must the response time be shorter or longer than the specified response time?"),
		Type:         action_kit_api.ActionParameterTypeString,
		Required:     extutil.Ptr(false),
		Order:        extutil.Ptr(14),
		DefaultValue: extutil.Ptr("NO_VERIFICATION"),
		Options: extutil.Ptr([]action_kit_api.ParameterOption{
			action_kit_api.ExplicitParameterOption{
				Label: "no verification",
				Value: "NO_VERIFICATION",
			},
			action_kit_api.ExplicitParameterOption{
				Label: "shorter than",
				Value: "SHORTER_THAN",
			},
			action_kit_api.ExplicitParameterOption{
				Label: "longer than",
				Value: "LONGER_THAN",
			},
		}),
	}
	responseTime = action_kit_api.ActionParameter{
		Name:         "responseTime",
		Label:        "Response Time",
		Description:  extutil.Ptr("The value for the response time verification."),
		Type:         action_kit_api.ActionParameterTypeDuration,
		Required:     extutil.Ptr(false),
		Order:        extutil.Ptr(15),
		DefaultValue: extutil.Ptr("500ms"),
	}
	targetSelectionParameter = action_kit_api.ActionParameter{
		Name:  "-",
		Label: "Filter HTTP Client Locations",
		Type:  action_kit_api.ActionParameterTypeTargetSelection,
		Order: extutil.Ptr(17),
	}
	maxConcurrent = action_kit_api.ActionParameter{
		Name:         "maxConcurrent",
		Label:        "Max concurrent requests",
		Description:  extutil.Ptr("Maximum count on parallel running requests. (min 1, max 10)"),
		Type:         action_kit_api.ActionParameterTypeInteger,
		DefaultValue: extutil.Ptr("5"),
		Required:     extutil.Ptr(true),
		Advanced:     extutil.Ptr(true),
		MinValue:     extutil.Ptr(1),
		MaxValue:     extutil.Ptr(10),
		Order:        extutil.Ptr(18),
	}
	clientSettings = action_kit_api.ActionParameter{
		Name:     "clientSettings",
		Label:    "HTTP Client Settings",
		Type:     action_kit_api.ActionParameterTypeHeader,
		Advanced: extutil.Ptr(true),
		Order:    extutil.Ptr(19),
	}
	followRedirects = action_kit_api.ActionParameter{
		Name:        "followRedirects",
		Label:       "Follow Redirects?",
		Description: extutil.Ptr("Should Redirects be followed?"),
		Type:        action_kit_api.ActionParameterTypeBoolean,
		Required:    extutil.Ptr(true),
		Advanced:    extutil.Ptr(true),
		Order:       extutil.Ptr(20),
	}
	connectTimeout = action_kit_api.ActionParameter{
		Name:         "connectTimeout",
		Label:        "Connection Timeout",
		Description:  extutil.Ptr("Connection Timeout for a single Call in seconds. Should be between 1 and 10 seconds."),
		Type:         action_kit_api.ActionParameterTypeDuration,
		DefaultValue: extutil.Ptr("5s"),
		Required:     extutil.Ptr(true),
		Advanced:     extutil.Ptr(true),
		Order:        extutil.Ptr(21),
	}
	readTimeout = action_kit_api.ActionParameter{
		Name:         "readTimeout",
		Label:        "Read Timeout",
		Description:  extutil.Ptr("Read Timeout for a single Call in seconds. Should be between 1 and 10 seconds."),
		Type:         action_kit_api.ActionParameterTypeDuration,
		DefaultValue: extutil.Ptr("5s"),
		Required:     extutil.Ptr(true),
		Advanced:     extutil.Ptr(true),
		Order:        extutil.Ptr(22),
	}
	insecureSkipVerify = action_kit_api.ActionParameter{
		Name:         "insecureSkipVerify",
		Label:        "Skip certificate verification",
		Description:  extutil.Ptr("Should the certificate verification be skipped?"),
		Type:         action_kit_api.ActionParameterTypeBoolean,
		DefaultValue: extutil.Ptr("false"),
		Required:     extutil.Ptr(false),
		Advanced:     extutil.Ptr(true),
		Order:        extutil.Ptr(23),
	}
	widgetsBackwardCompatiblity = extutil.Ptr([]action_kit_api.Widget{
		action_kit_api.PredefinedWidget{
			Type:               action_kit_api.ComSteadybitWidgetPredefined,
			PredefinedWidgetId: "com.steadybit.widget.predefined.HttpCheck",
		},
	})
	widgets = extutil.Ptr([]action_kit_api.Widget{
		action_kit_api.LineChartWidget{
			Type:  action_kit_api.ComSteadybitWidgetLineChart,
			Title: "HTTP Responses",
			Identity: action_kit_api.LineChartWidgetIdentityConfig{
				MetricName: "response_time",
				From:       "url",
				Mode:       action_kit_api.ComSteadybitWidgetLineChartIdentityModeWidgetPerValue,
			},
			Grouping: extutil.Ptr(action_kit_api.LineChartWidgetGroupingConfig{
				ShowSummary: extutil.Ptr(true),
				Groups: []action_kit_api.LineChartWidgetGroup{
					{
						Title: "Successul",
						Color: "success",
						Matcher: action_kit_api.LineChartWidgetGroupMatcherFallback{
							Type: action_kit_api.ComSteadybitWidgetLineChartGroupMatcherFallback,
						},
					},
					{
						Title: "Failure",
						Color: "warn",
						Matcher: action_kit_api.LineChartWidgetGroupMatcherNotEmpty{
							Type: action_kit_api.ComSteadybitWidgetLineChartGroupMatcherNotEmpty,
							Key:  "error",
						},
					},
					{
						Title: "Unexpected Status",
						Color: "warn",
						Matcher: action_kit_api.LineChartWidgetGroupMatcherKeyEqualsValue{
							Type:  action_kit_api.ComSteadybitWidgetLineChartGroupMatcherKeyEqualsValue,
							Key:   "expected_http_status",
							Value: "false",
						},
					},
					{
						Title: "Body Constraint Violated",
						Color: "warn",
						Matcher: action_kit_api.LineChartWidgetGroupMatcherKeyEqualsValue{
							Type:  action_kit_api.ComSteadybitWidgetLineChartGroupMatcherKeyEqualsValue,
							Key:   "response_constraints_fulfilled",
							Value: "false",
						},
					},
					{
						Title: "Response Time Constraint Violated",
						Color: "warn",
						Matcher: action_kit_api.LineChartWidgetGroupMatcherKeyEqualsValue{
							Type:  action_kit_api.ComSteadybitWidgetLineChartGroupMatcherKeyEqualsValue,
							Key:   "response_time_constraints_fulfilled",
							Value: "false",
						},
					},
				},
			}),
			Tooltip: extutil.Ptr(action_kit_api.LineChartWidgetTooltipConfig{
				MetricValueTitle: extutil.Ptr("Response Time"),
				MetricValueUnit:  extutil.Ptr("ms"),
				AdditionalContent: []action_kit_api.LineChartWidgetTooltipContent{
					{
						From:  "error",
						Title: "Error",
					},
					{
						From:  "http_status",
						Title: "HTTP Status",
					},
				},
			}),
		},
	})
)

func separator(order int) action_kit_api.ActionParameter {
	return action_kit_api.ActionParameter{
		Name:  "-",
		Label: "-",
		Type:  action_kit_api.ActionParameterTypeSeparator,
		Order: extutil.Ptr(order),
	}
}

func filter[T any](ss []T, test func(T) bool) (ret []T) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}
