package appcore

// AppVersion — версия приложения. Единственный источник правды для интерфейса,
// трея на Windows и раздела «О программе» на Android.
//
// При выпуске бампится здесь и ещё в двух местах, которые Go не видит:
// `info.productVersion` в wails.json (ресурс версии в exe) и
// `versionName`/`versionCode` в android/app/build.gradle.kts. Разъехавшиеся
// значения = «в программе 2.1.0, в свойствах файла 1.3.1».
const AppVersion = "2.1.2"
