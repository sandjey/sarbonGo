--
-- PostgreSQL database dump
--


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
-- Name: regions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.regions (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    code character varying(20) NOT NULL,
    name_ru character varying(255) NOT NULL,
    name_en character varying(255),
    country_code character varying(3) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: regions regions_country_code_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.regions
    ADD CONSTRAINT regions_country_code_code_key UNIQUE (country_code, code);


--
-- Name: regions regions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.regions
    ADD CONSTRAINT regions_pkey PRIMARY KEY (id);


--
-- Name: idx_regions_country; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_regions_country ON public.regions USING btree (country_code);


--
-- PostgreSQL database dump complete
--


