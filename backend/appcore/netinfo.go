package appcore

// Внешний IP и гео — как их видит интернет после подключения.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// IPInfo — внешний IP и его гео, полученные через прокси.
type IPInfo struct {
	IP          string `json:"ip"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	City        string `json:"city"`
}

// ExternalIP запрашивает внешний IP и страну ЧЕРЕЗ локальный mixed-прокси — то есть
// показывает, откуда «видит» пользователя интернет после подключения. Пробуем
// несколько сервисов: если один недоступен/лимитит — берём следующий.
func (c *Core) ExternalIP() (IPInfo, error) {
	proxyURL, _ := url.Parse("http://" + c.ProxyAddr())
	hc := &http.Client{
		Timeout:   7 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	for _, provider := range []func(*http.Client) (IPInfo, error){geoIPAPI, geoIPWhoIs, geoIPInfo} {
		if info, err := provider(hc); err == nil && info.IP != "" {
			return info, nil
		}
	}
	return IPInfo{}, CodedErr(ErrIPUnknown, "не удалось определить внешний IP")
}

// geoIPAPI — ip-api.com (полное имя страны + код + город; HTTP, но без ключа).
func geoIPAPI(hc *http.Client) (IPInfo, error) {
	resp, err := hc.Get("http://ip-api.com/json/?fields=status,country,countryCode,city,query")
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	var r struct{ Status, Country, CountryCode, City, Query string }
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return IPInfo{}, err
	}
	if r.Status != "success" {
		return IPInfo{}, fmt.Errorf("ip-api: %s", r.Status)
	}
	return IPInfo{IP: r.Query, Country: r.Country, CountryCode: r.CountryCode, City: r.City}, nil
}

// geoIPWhoIs — ipwho.is (HTTPS, полное имя + код + город).
func geoIPWhoIs(hc *http.Client) (IPInfo, error) {
	resp, err := hc.Get("https://ipwho.is/")
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	var r struct {
		IP          string `json:"ip"`
		Success     bool   `json:"success"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		City        string `json:"city"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return IPInfo{}, err
	}
	if !r.Success {
		return IPInfo{}, fmt.Errorf("ipwho.is: fail")
	}
	return IPInfo{IP: r.IP, Country: r.Country, CountryCode: r.CountryCode, City: r.City}, nil
}

// geoIPInfo — ipinfo.io (HTTPS; страна только кодом, но как надёжный фолбэк).
func geoIPInfo(hc *http.Client) (IPInfo, error) {
	resp, err := hc.Get("https://ipinfo.io/json")
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	var r struct{ IP, Country, City string }
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return IPInfo{}, err
	}
	if r.IP == "" {
		return IPInfo{}, fmt.Errorf("ipinfo: пусто")
	}
	// Country тут — двухбуквенный код; полного имени нет.
	return IPInfo{IP: r.IP, Country: "", CountryCode: r.Country, City: r.City}, nil
}
