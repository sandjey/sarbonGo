--
-- PostgreSQL database dump
--

\restrict jTdQxspVh4CgUO1CXbyMDxa0axhkp09XeQGeU7pMyRk9IXVOC5HwOgsYH8Nqb75

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: geo_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.geo_stats (
    day_utc date NOT NULL,
    geo_city character varying(128) NOT NULL,
    role character varying(32) NOT NULL,
    user_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    event_count bigint DEFAULT 0 NOT NULL,
    login_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: geo_stats geo_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.geo_stats
    ADD CONSTRAINT geo_stats_pkey PRIMARY KEY (day_utc, geo_city, role, user_id);


--
-- Name: idx_geo_stats_day_city; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_geo_stats_day_city ON public.geo_stats USING btree (day_utc DESC, geo_city);


--
-- PostgreSQL database dump complete
--

\unrestrict jTdQxspVh4CgUO1CXbyMDxa0axhkp09XeQGeU7pMyRk9IXVOC5HwOgsYH8Nqb75

